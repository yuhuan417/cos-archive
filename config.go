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
	Index      string
	COS        COSConfig
}

// COSConfig struct
type COSConfig struct {
	URL         string
	ID          string
	Key         string
	ChunkPrefix string
}

func getConfig(configFileName string) (config Config) {
	// default value
	config.Threads = 4
	config.Index = "meta.json"
	config.COS.ChunkPrefix = "data/"

	jsonFile, err := os.Open(configFileName)

	if err != nil {
		return
	}

	defer jsonFile.Close()

	byteValue, _ := ioutil.ReadAll(jsonFile)

	json.Unmarshal(byteValue, &config)

	return
}
