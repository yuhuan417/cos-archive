package main

import (
	"encoding/json"
	"io/ioutil"
	"os"
)

// Config struct
type Config struct {
	FilePaths  []string
	SkipList   []string
	Threads    int
	WorkingDir string
	TargetDir  string
	Index      string
	Port       string
	COS        COSConfig
}

// COSConfig struct
type COSConfig struct {
	URL         string
	ID          string
	Key         string
	ChunkPrefix string
	Class       string
	Retries     int
}

func getConfig(configFileName string) (config Config) {
	// default value
	config.Threads = 4
	config.Index = "meta.json"
	config.COS.ChunkPrefix = "data/"
	config.COS.Class = "DEEP_ARCHIVE"
	config.COS.Retries = 1

	jsonFile, err := os.Open(configFileName)

	if err != nil {
		return
	}

	defer jsonFile.Close()

	byteValue, _ := ioutil.ReadAll(jsonFile)

	json.Unmarshal(byteValue, &config)

	return
}
