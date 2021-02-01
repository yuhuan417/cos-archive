package main

import (
	"io/ioutil"
	"os"
	"encoding/json"
)

type Config struct {
	FilePaths []string
	SkipList []string
	Threads	int
	Oss          ossConfig
}

type ossConfig struct {
	OssKey     string
	OssSecret  string
	BucketName string
	APIPrefix  string
}

func getConfig(configFileName string) (config Config) {
	// config.FilePaths = []string{"aa", "bb"}
	// config.Oss.OssKey = "k"
	// config.Oss.OssSecret = "s"
	// config.Oss.BucketName = "b"
	// config.Oss.APIPrefix = "a"

	// f, _ := json.Marshal(config)
	// fmt.Println(string(f))

	// default value
	config.Threads = 4

	jsonFile, err := os.Open(configFileName)

	if (err != nil) {
		return
	}

	defer jsonFile.Close()

	byteValue, _ := ioutil.ReadAll(jsonFile)

	json.Unmarshal(byteValue, &config)

	return
}
