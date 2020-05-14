package main

import (
	"bytes"
	"fmt"
	lru "github.com/hashicorp/golang-lru"
	strftime "github.com/jamesjj/strftime"
)

type (
	MatchMapType map[string]map[string]map[string]string
	PInstance    struct {
		Config        *Config
		EventJsonChan chan *[]byte
		ToPostChan    chan *bytes.Buffer
		HttpClient    *HttpClient
		Log           *SimpleLogger
		LRU           *lru.Cache
		MatchMap      MatchMapType
		TimeFormatter *strftime.Strftime
	}
	PInstances map[string]*PInstance
)

func makePInstance(conf *Config) (*PInstance, error) {

	log := Logger(conf.LogLevel, fmt.Sprintf("[%s] [%s] ", PLUGIN_NAME, conf.Id))

	eventJsonChan := make(chan *[]byte, conf.MaxRecords)

	toPostChan := make(chan *bytes.Buffer, conf.MaxRecords)

	timeFormatter, tfErr := strftime.New(
		conf.OutputTimeFormat,
		strftime.WithMilliseconds('L'),
		strftime.WithUnixSeconds('s'),
	)
	if tfErr != nil {
		return nil, fmt.Errorf(
			"Invalid `output_time_format`: %v (%s)",
			tfErr,
			conf.OutputTimeFormat,
		)
	}

	newLru, lruErr := lru.New(conf.DeduplicateSize)
	if lruErr != nil {
		return nil, fmt.Errorf(
			"Failed to create LRU: %v",
			lruErr,
		)
	}

	var matchMap MatchMapType
	if mmfErr := loadMatchMapFile(conf.MatchMapFile, &matchMap); mmfErr != nil {
		return nil, fmt.Errorf(
			"Failed to load match map file: %v",
			mmfErr,
		)
	}

	hc, hcErr := httpClient()
	if hcErr != nil {
		return nil, fmt.Errorf(
			"Failed initialize HTTP client: %v",
			hcErr,
		)
	}

	return &PInstance{
		Config:        conf,
		EventJsonChan: eventJsonChan,
		ToPostChan:    toPostChan,
		HttpClient:    hc,
		Log:           log,
		LRU:           newLru,
		MatchMap:      matchMap,
		TimeFormatter: timeFormatter,
	}, nil

}
