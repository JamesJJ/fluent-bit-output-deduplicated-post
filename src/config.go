package main

import (
	"fmt"
	output "github.com/fluent/fluent-bit-go/output"
	"unsafe"
)

const (
	PLUGIN_NAME = "http_post"
)

type (
	Config struct {
		Id                   string
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
		OutputTimeAsInteger  bool
		Headers              *map[string]string
	}
)

func configFromFLB(plugin unsafe.Pointer) (*Config, error) {

	flbCK := func(k string) string {
		return output.FLBPluginConfigKey(plugin, k)
	}

	deduplicate_key_fields := []string{}
	csvAppend(flbCK("deduplicate_key_fields"), &deduplicate_key_fields)

	deduplicate_size := int(parseInteger(flbCK("deduplicate_size"), 1024))

	deduplicate_ttl := parseInteger(flbCK("deduplicate_ttl"), 86400*7)

	gzip_body := parseBool(flbCK("gzip_body"), true)

	id := flbCK("id")
	if len(id) < 1 {
		return nil, fmt.Errorf("[%s] Missing `Id` in [OUTPUT] config\n", PLUGIN_NAME)
	}

	log := flbCK("log")
	if len(log) < 4 {
		log = "info"
	}

	match_map_file := flbCK("match_map_file")

	max_records := parseInteger(flbCK("max_records"), 20)

	output_time_format := flbCK("output_time_format")

	output_time_integer := parseBool(flbCK("output_time_integer"), false)

	output_time_key := flbCK("output_time_key")

	post_headers := &map[string]string{"Content-Type": "application/octets"}

	post_url := flbCK("post_url")
	if !meaningfulUrl(post_url) {
		return nil, fmt.Errorf("Invalid `post_url`: %+v", post_url)
	}

	remove_fields := []string{}
	csvAppend(flbCK("remove_fields"), &remove_fields)

	return &Config{
		Id:                   id,
		LogLevel:             log,
		PostUrl:              post_url,
		GzipBody:             gzip_body,
		MaxRecords:           max_records,
		MatchMapFile:         match_map_file,
		DeduplicateKeyFields: deduplicate_key_fields,
		DeduplicateSize:      deduplicate_size,
		DeduplicateTTL:       deduplicate_ttl,
		RemoveFields:         remove_fields,
		OutputTimeKey:        output_time_key,
		OutputTimeFormat:     output_time_format,
		OutputTimeAsInteger:  output_time_integer,
		Headers:              post_headers,
	}, nil
}
