package main

import (
	"encoding/json"
	"io/ioutil"
	"os"
)

type Config struct {
	FilePaths []string
	SkipList  []string
	Threads   int
	WorkingDir string
	Index	string
	COS       COSConfig
}

type COSConfig struct {
	URL    string
	ID     string
	Key    string
	ChunkPrefix string
}

func getConfig(configFileName string) (config Config) {
	// default value
	config.Threads = 2
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
