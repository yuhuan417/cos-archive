package main

import (
	"log"
	"path"

	"github.com/tencentyun/cos-go-sdk-v5"
	"go.uber.org/ratelimit"
)

func logRestoreStatus(err error) {
	if err == nil {
		return
	}
	if cos.IsNotFoundError(err) {
		// WARN
		log.Println("WARN: Resource is not existed")
	} else if e, ok := cos.IsCOSError(err); ok {
		if e.Code == "RestoreAlreadyInProgress" {
			return
		}
		log.Printf("ERROR: Code: %v\n", e.Code)
		log.Printf("ERROR: Message: %v\n", e.Message)
		log.Printf("ERROR: Resource: %v\n", e.Resource)
		log.Printf("ERROR: RequestId: %v\n", e.RequestID)
		// ERROR
	} else {
		log.Printf("ERROR: %v\n", err)
		// ERROR
	}
}

func restoreChunk(config Config, cm ChunksMap) {
	log.Println("Restoring chunks")
	// Download chunk from map
	rl := ratelimit.New(90) // per second, hardcode.

	for k := range cm {
		rl.Take()
		p := path.Join(config.COS.ChunkPrefix, k)
		c := NewCOS(config.COS)
		err := c.RestoreFile(p)
		logRestoreStatus(err)
	}
}

func restoreFiles(config Config) {
	cm := scanRemoteChunksMap(config)
	restoreChunk(config, cm)
}
