package main

import (
	"C"
	"encoding/json"
	output "github.com/fluent/fluent-bit-go/output"
	"strings"
	"sync"
	"time"
	"unsafe"
)

var (
	flbInstances PInstances
	wg           sync.WaitGroup
)

//export FLBPluginRegister
func FLBPluginRegister(def unsafe.Pointer) int {
	flbInstances = make(PInstances)
	return output.FLBPluginRegister(def, PLUGIN_NAME, "Custom HTTP POST output")
}

//export FLBPluginInit
func FLBPluginInit(plugin unsafe.Pointer) int {

	conf, confErr := configFromFLB(plugin)
	if confErr != nil {
		Logger("info", "").Error.Printf("[%s] Configuration problem: %v\n", PLUGIN_NAME, confErr)
		return output.FLB_ERROR
	}

	output.FLBPluginSetContext(plugin, conf.Id)

	pInstance, piErr := makePInstance(conf)
	if piErr != nil {
		Logger("info", "").Error.Printf("[%s] Instance problem: %v\n", PLUGIN_NAME, piErr)
		return output.FLB_ERROR
	}
	flbInstances[conf.Id] = pInstance

	log := pInstance.Log

	if json, jsonErr := json.Marshal(conf); jsonErr == nil {
		log.Info.Printf("Configuration => %s\n", json)
	}

	log.Debug.Printf(
		"Current time in `output_time_format` => %s\n",
		*formattedTime(
			pInstance.TimeFormatter,
			time.Now(),
		).String,
	)

	wg.Add(1)
	go func(pi *PInstance, wg *sync.WaitGroup) {
		defer wg.Done()
		aggregateChannelLoop(
			pi.Log,
			2000,
			pi.Config.MaxRecords,
			pi.Config.GzipBody,
			pi.EventJsonChan,
			pi.ToPostChan,
		)
	}(pInstance, &wg)

	wg.Add(1)
	go func(pi *PInstance, wg *sync.WaitGroup) {
		defer wg.Done()
		doPostLoop(
			pi.Log,
			pi.HttpClient,
			pi.Config.PostUrl,
			pi.Config.Headers,
			pi.ToPostChan,
		)
	}(pInstance, &wg)

	return output.FLB_OK
}

//export FLBPluginFlush
func FLBPluginFlush(data unsafe.Pointer, length C.int, tag *C.char) int {
	// As we enforce providing `Id`, this should never occur
	Logger("info", "").Error.Printf("Flush called for unknown instance\n")
	return output.FLB_ERROR
}

//export FLBPluginFlushCtx
func FLBPluginFlushCtx(ctx, data unsafe.Pointer, length C.int, tag *C.char) int {

	id := output.FLBPluginGetContext(ctx).(string)
	pi := flbInstances[id]
	log := pi.Log
	conf := pi.Config

	log.Debug.Printf("Flush called\n")

	dec := output.NewDecoder(data, int(length))

	count := 0
	for {
		count++
		ret, ts, record := output.GetRecord(dec)
		if ret != 0 {
			break
		}

		timestamp := ts.(output.FLBTime)
		timestampAsTime := timestamp.Time
		timeNow := time.Now()
		recordAge := timeNow.Sub(timestampAsTime)

		// If record timestamp is too old or more than 1h in future, then discard it!
		if recordAge > time.Duration(conf.DeduplicateTTL)*time.Second ||
			timestampAsTime.After(timeNow.Add(1*time.Hour)) {
			log.Error.Printf(
				"Record too old (or in future): recordIndex=%d, recordAge=%s, timestamp=%s\n",
				count,
				recordAge,
				timestampAsTime,
			)
			continue
		}

		// Simplify incoming event to a string map
		var stringified StringifiedRecordType
		if result, err := stringifyFluentBitRecord(record); err == nil {
			stringified = result
		} else {
			log.Error.Printf(
				"Failed to understand: recordIndex=%d, tag=%s, time=%s, record=%v\n",
				count,
				C.GoString(tag),
				timestampAsTime,
				record,
			)
			continue
		}

		// log the stringified record for debugging
		log.Debug.Printf(
			"recordIndex=%d, tag=%s, time=%s, record=%v\n",
			count,
			C.GoString(tag),
			timestampAsTime,
			stringified,
		)

		// check if the record matches the fields in the match map file
		if ok, matched, additionalFields, _ := matchRecordToMatchMap(
			stringified,
			pi.MatchMap,
		); !ok {
			// the record did not match the match map
			log.Debug.Printf(
				"Record did not match: recordIndex=%d\n",
				count,
			)
			continue
		} else {
			// the record did match a field in the match map
			log.Debug.Printf(
				"recordIndex=%d, matched=%v, additionalFields=%v\n",
				count,
				*matched,
				*additionalFields,
			)
			// add any additional fields from the match map to the record
			for k, v := range *additionalFields {
				stringified[k] = v
			}
		}

		// generate a key for use with the LRU cache
		lruKey := generateDeduplicationKeyFromRecordValues(
			conf.DeduplicateKeyFields,
			stringified,
		)

		// Get any existing record for this key from LRU
		existingFromLru, foundInLruBool := pi.LRU.Get(lruKey)
		log.Debug.Printf(
			"recordIndex=%d, LRU: key=%s, foundInLruBool=%v, value=%v\n",
			count,
			lruKey,
			foundInLruBool,
			existingFromLru,
		)

		if foundInLruBool {
			lruRecordTime := existingFromLru.(time.Time)
			lruRecordAge := timeNow.Sub(lruRecordTime)
			// If record from LRU is recent, we don't need to process the current record
			if lruRecordAge < time.Duration(conf.DeduplicateTTL)*time.Second {
				log.Debug.Printf(
					"Skipping send as recent in LRU: recordIndex=%d, lruRecordAge=%s\n",
					count,
					lruRecordAge,
				)
				continue
			}
		}

		// Add the current record to the LRU
		if evicted := pi.LRU.Add(lruKey, timestampAsTime); evicted {
			log.Info.Printf(
				"LRU evicted old record\n",
			)
		}

		// remove any undesired fields prior to forwarding
		for _, removeFieldKey := range conf.RemoveFields {
			delete(stringified, removeFieldKey)
		}

		// add any time field to the outgoing record
		if timeKey := strings.TrimSpace(conf.OutputTimeKey); len(timeKey) > 0 {
			if timeValue := formattedTime(
				pi.TimeFormatter,
				timestampAsTime,
			); conf.OutputTimeAsInteger {
				stringified[timeKey] = *timeValue.Int64
			} else {
				stringified[timeKey] = *timeValue.String
			}
		}

		// Marshal to JSON and put in to channel
		if json, err := json.Marshal(stringified); err == nil {
			log.Info.Printf("Sending => %s\n", json)
			pi.EventJsonChan <- &json
		} else {
			log.Error.Printf("Failed to marshal as JSON: %v (%#v)\n", err, stringified)
		}

	}

	return output.FLB_OK
}

//export FLBPluginExit
func FLBPluginExit() int {
	for k := range flbInstances {
		close(flbInstances[k].EventJsonChan)
	}
	wg.Wait()
	return output.FLB_OK
}
