package main

import (
	"bytes"
	"compress/gzip"
	"sync"
	"time"
)

var (
	bufPool = sync.Pool{
		New: func() interface{} {
			return new(bytes.Buffer)
		},
	}
	gzipWriter *gzip.Writer
)

func returnToPool(b *bytes.Buffer) {
	bufPool.Put(b)
}

func doPostLoop(
	log *SimpleLogger,
	hc *HttpClient,
	url string,
	headers *map[string]string,
	inChan chan *bytes.Buffer,
) error {

	for {
		chunk, ok := <-inChan
		if !ok {
			return nil
		}
		// TODO/Future: catch error and retry
		hc.postData(log, url, headers, chunk)
		returnToPool(chunk)
	}
	return nil
}

func aggregateChannelLoop(
	log *SimpleLogger,
	chunkPeriod uint64,
	chunkMaxSize uint64,
	compress bool,
	inChan chan *[]byte,
	outChan chan *bytes.Buffer,
) {
	chunkTimeStep := uint64(100)
	newLine := []byte("\n")
	bufPointer := bufPool.Get().(*bytes.Buffer)
	if compress {
		gzipWriter = gzip.NewWriter(bufPointer)
	}
	for {
		chunkSize := uint64(0)
		chunkTime := uint64(0)
		bufPointer = bufPool.Get().(*bytes.Buffer)
		bufPointer.Reset()
		if compress {
			gzipWriter.Reset(bufPointer)
		}
		for (chunkSize < chunkMaxSize) && (chunkTime < chunkPeriod) {
			select {
			case event, ok := <-inChan:
				if !ok {
					close(outChan)
					return
				}
				chunkSize++
				if compress {
					if _, err := gzipWriter.Write(*event); err != nil {
						log.Error.Printf("Gzip write error: %v", err)
						break
					}
					if _, err := gzipWriter.Write(newLine); err != nil {
						log.Error.Printf("Gzip write error: %v", err)
						break
					}
				} else {
					if _, err := bufPointer.Write(*event); err != nil {
						log.Error.Printf("Buffer write error: %v", err)
						break
					}
					if _, err := bufPointer.Write(newLine); err != nil {
						log.Error.Printf("Buffer write error: %v", err)
						break
					}
				}
			default:
				time.Sleep(time.Duration(chunkTimeStep) * time.Millisecond)
				chunkTime += chunkTimeStep
			}
		}

		if compress {
			if err := gzipWriter.Flush(); err != nil {
				log.Error.Printf("GZ flush error: %+v", err)
			}
			if err := gzipWriter.Close(); err != nil {
				log.Error.Printf("GZ close error: %+v", err)
			}
		}
		if chunkSize > 0 {
			log.Debug.Printf(
				"Aggregated chunk with %d records, and size %d bytes (compressed: %v)",
				chunkSize,
				bufPointer.Len(),
				compress,
			)
			outChan <- bufPointer
		}
	}
}
