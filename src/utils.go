package main

import (
	"encoding/json"
	"fmt"
	strftime "github.com/jamesjj/strftime"
	"io/ioutil"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const ()

type (
	StringifiedRecordType map[string]interface{}

	StringInt64 struct {
		String *string
		Int64  *int64
	}
)

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

func meaningfulUrl(str string) bool {
	u, err := url.Parse(str)
	return err == nil && u.Scheme != "" && u.Host != ""
}

func csvAppend(s string, l *[]string) {
	for _, v := range strings.Split(s, ",") {
		*l = append(*l, strings.TrimSpace(v))
	}
}

func formattedTime(tf *strftime.Strftime, t time.Time) StringInt64 {
	timeString := tf.FormatString(t)
	if i, err := strconv.ParseInt(timeString, 10, 64); err == nil {
		return StringInt64{
			String: &timeString,
			Int64:  &i,
		}
	} else {
		return StringInt64{
			String: &timeString,
			Int64:  nil,
		}
	}
}

// TODO/Future: MMF should be optional
func matchRecordToMatchMap(stringifiedRecord StringifiedRecordType, matchMap MatchMapType) (bool, *map[string]string, *map[string]string, error) {
	WILDCARD := "*"
	matched := make(map[string]string)
	for mmKey, mmChild := range matchMap {
		if _, keyExists := stringifiedRecord[mmKey]; keyExists == true {
			for mmChildKey, mmChildValuesMap := range mmChild {
				if strings.HasSuffix(mmChildKey, WILDCARD) {
					prefixToMatch := strings.Split(mmChildKey, WILDCARD)[0]
					if strings.HasPrefix(stringifiedRecord[mmKey].(string), prefixToMatch) {
						matched[mmKey] = prefixToMatch + WILDCARD
						return true, &matched, &mmChildValuesMap, nil
					}
				} else {
					if stringifiedRecord[mmKey].(string) == mmChildKey {
						matched[mmKey] = mmChildKey
						return true, &matched, &mmChildValuesMap, nil
					}
				}
			}
		}
	}
	return false, nil, nil, nil
}

// TODO/Future: reloading / config from URL
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

func generateDeduplicationKeyFromRecordValues(ddFields []string, record StringifiedRecordType) string {
	var str strings.Builder
	for i, v := range ddFields {
		str.WriteString(record[v].(string))
		if i+1 < len(ddFields) {
			str.WriteString(":")
		}
	}
	return str.String()
}
