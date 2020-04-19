package main

import (
	"C"
	"encoding/json"
	"fmt"
	output "github.com/fluent/fluent-bit-go/output"
	lru "github.com/hashicorp/golang-lru"
	"io/ioutil"
	"strconv"
	"strings"
	"time"
	"unsafe"
)

const (
	PLUGIN_NAME = "http_post"
)

type (
	Config struct {
		LogLevel             string
		PostUrl              string
		GzipBody             bool
		MaxRecords           uint64
		MatchMapFile         string
		DeduplicateKeyFields []string
		DeduplicateSize      int
		DeduplicateTTL       uint64
		RemoveFields         []string
		OutputTimeKey        string
		OutputTimeFormat     string
		LRU                  *lru.Cache `json:"-"`
	}

	Configs map[string]*Config

	StringifiedRecordType map[string]string

	MatchMapType map[string]map[string]map[string]string
)

var (
	Log      *SimpleLogger
	conf     Configs
	matchMap MatchMapType
)

//export FLBPluginRegister
func FLBPluginRegister(def unsafe.Pointer) int {
	conf = make(Configs)
	return output.FLBPluginRegister(def, PLUGIN_NAME, "Custom HTTP POST output")
}

//export FLBPluginInit
func FLBPluginInit(plugin unsafe.Pointer) int {

	flbCK := func(k string) string {
		return output.FLBPluginConfigKey(plugin, k)
	}

	id := flbCK("id")
	if len(id) < 1 {
		Logger("info", "").Error.Printf("[%s] Missing `Id` in [OUTPUT] config\n", PLUGIN_NAME)
		return output.FLB_ERROR
	}
	output.FLBPluginSetContext(plugin, id)

	conf[id] = &Config{}

	conf[id].LogLevel = flbCK("log")
	if len(conf[id].LogLevel) < 4 {
		conf[id].LogLevel = "info"
	}

	Log = Logger(conf[id].LogLevel, fmt.Sprintf("[%s] [%s] ", PLUGIN_NAME, id))

	conf[id].MaxRecords = parseInteger(flbCK("max_records"), 20)
	conf[id].DeduplicateSize = int(parseInteger(flbCK("deduplicate_size"), 1024))
	conf[id].DeduplicateTTL = parseInteger(flbCK("deduplicate_ttl"), 86400*7)
	conf[id].GzipBody = parseBool(flbCK("gzip_body"), true)
	conf[id].PostUrl = flbCK("post_url")
	conf[id].MatchMapFile = flbCK("match_map_file")
	conf[id].OutputTimeKey = flbCK("output_time_key")
	conf[id].OutputTimeFormat = flbCK("output_time_format")
	csvAppend(flbCK("deduplicate_key_fields"), &conf[id].DeduplicateKeyFields)
	csvAppend(flbCK("remove_fields"), &conf[id].RemoveFields)

	if len(conf[id].PostUrl) == 0 {
		Log.Error.Printf("Specifying `post_url` is mandatory")
		return output.FLB_ERROR
	}

	if conf[id].DeduplicateSize+len(conf[id].DeduplicateKeyFields) > 0 && (conf[id].DeduplicateSize == 0 || len(conf[id].DeduplicateKeyFields) == 0) {
		Log.Error.Printf("Specify both `deduplicate_key_fields` and `deduplicate_size`, or neither\n")
		return output.FLB_ERROR
	}

	Log.Info.Printf("Configuration => %+v\n", *conf[id])

	if newLru, err := lru.New(conf[id].DeduplicateSize); err == nil {
		conf[id].LRU = newLru
	} else {
		Log.Error.Printf("Failed to create LRU: %#v\n", err)
		return output.FLB_ERROR
	}

	if err := loadMatchMapFile(conf[id].MatchMapFile, &matchMap); err != nil {
		Log.Error.Printf("Failed to load match map file: %+v\n", err)
		return output.FLB_ERROR
	}

	return output.FLB_OK
}

//export FLBPluginFlush
func FLBPluginFlush(data unsafe.Pointer, length C.int, tag *C.char) int {
	Log.Error.Printf("Flush called for unknown instance\n")
	// As we enforce providing `Id`, this should never occur
	return output.FLB_ERROR
}

//export FLBPluginFlushCtx
func FLBPluginFlushCtx(ctx, data unsafe.Pointer, length C.int, tag *C.char) int {
	// Type assert context back into the original type for the Go variable
	id := output.FLBPluginGetContext(ctx).(string)

	Log.Debug.Printf("Flush called\n")

	dec := output.NewDecoder(data, int(length))

	count := 0
	for {
		count++
		ret, ts, record := output.GetRecord(dec)
		if ret != 0 {
			break
		}

		timestamp := ts.(output.FLBTime)

		var stringified StringifiedRecordType
		if result, err := stringifyFluentBitRecord(record); err == nil {
			stringified = result
		} else {
			Log.Error.Printf("recordIndex=%d, tag=%s, time=%s, record=%v\n", count, C.GoString(tag), timestamp.String(), record)
			continue
		}

		Log.Debug.Printf("recordIndex=%d, tag=%s, time=%s, record=%v\n", count, C.GoString(tag), timestamp.String(), stringified)

		if ok, matched, additionalFields, _ := matchRecordToMatchMap(stringified, matchMap); ok == true {
			Log.Debug.Printf("recordIndex=%d, matched=%v, additionalFields=%v\n", count, *matched, *additionalFields)
			for k, v := range *additionalFields {
				stringified[k] = v
			}
		} else {
			Log.Debug.Printf("recordIndex=%d, Record did not match\n", count)
			continue
		}

		lruKey := generateDeduplicationKeyFromRecordValues(&conf[id].DeduplicateKeyFields, stringified)
		existing, present := conf[id].LRU.Get(lruKey)
		Log.Debug.Printf("recordIndex=%d, LRU: key=%s, present=%v, value=%#v\n", count, lruKey, present, existing)

		// TODO: validate record time is sane
		timeNow := time.Now()
		if present {
			timeBefore := existing.(time.Time)
			// TODO TTL from config
			if timeBefore.Add(time.Duration(1000) * time.Second).After(timeNow) {
				Log.Debug.Printf("recordIndex=%d, lruTime=%s, timeNow=%s, skipping record send\n", count, existing, timeNow)
				continue
			}
		}

		if evicted := conf[id].LRU.Add(lruKey, timeNow); evicted == true {
			Log.Info.Printf("timeNow=%s, LRU evicted old record\n", timeNow)
		}

		for _, removeFieldKey := range conf[id].RemoveFields {
			delete(stringified, removeFieldKey)
		}

		// TODO: add "timestamp" field to stringified

		//TODO move to goroutine
		if json, err := json.Marshal(stringified); err == nil {
			Log.Info.Printf("SENDING => %s\n", json)
		} else {
			Log.Error.Printf("Failed to marshal as JSON: %#v\n", err)
		}

	}

	return output.FLB_OK
}

//export FLBPluginExit
func FLBPluginExit() int {
	return output.FLB_OK
}

func parseInteger(s string, d uint64) uint64 {
	if parsedNumber, err := strconv.ParseUint(s, 10, 64); err != nil || len(s) == 0 {
		return d
	} else {
		return parsedNumber
	}
}

func parseBool(s string, d bool) bool {
	if parsedToBool, err := strconv.ParseBool(s); err == nil {
		return parsedToBool
	}
	return d
}

func csvAppend(s string, l *[]string) {
	for _, v := range strings.Split(s, ",") {
		*l = append(*l, strings.TrimSpace(v))
	}
}

// TODO: MMF should be optional
func matchRecordToMatchMap(stringifiedRecord StringifiedRecordType, matchMap MatchMapType) (bool, *map[string]string, *map[string]string, error) {
	WILDCARD := "*"
	matched := make(map[string]string)
	for mmKey, mmChild := range matchMap {
		if _, keyExists := stringifiedRecord[mmKey]; keyExists == true {
			for mmChildKey, mmChildValuesMap := range mmChild {
				if strings.HasSuffix(mmChildKey, WILDCARD) {
					prefixToMatch := strings.Split(mmChildKey, WILDCARD)[0]
					if strings.HasPrefix(stringifiedRecord[mmKey], prefixToMatch) {
						matched[mmKey] = prefixToMatch + WILDCARD
						return true, &matched, &mmChildValuesMap, nil
					}
				} else {
					if stringifiedRecord[mmKey] == mmChildKey {
						matched[mmKey] = mmChildKey
						return true, &matched, &mmChildValuesMap, nil
					}
				}
			}
		}
	}
	return false, nil, nil, nil
}

// TODO reloading / config from URL
func loadMatchMapFile(filename string, target interface{}) error {
	raw, err := ioutil.ReadFile(filename)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return err
	}
	return nil
}

func stringifyFluentBitRecord(record map[interface{}]interface{}) (StringifiedRecordType, error) {
	stringified := make(StringifiedRecordType)
	for k, v := range record {
		if kStr, kOk := k.(string); kOk {
			if vStr, vOk := v.([]byte); vOk {
				stringified[kStr] = string(vStr)
			} else {
				return nil, fmt.Errorf("Unable to stringify value: %#v", v)
			}

		} else {
			return nil, fmt.Errorf("Unable to stringify key: %#v", k)
		}
	}
	return stringified, nil
}

func generateDeduplicationKeyFromRecordValues(ddFields *[]string, record StringifiedRecordType) string {
	var str strings.Builder
	for i, v := range *ddFields {
		str.WriteString(record[v])
		if i+1 < len(*ddFields) {
			str.WriteString(":")
		}
	}
	return str.String()
}

func main() {
}
