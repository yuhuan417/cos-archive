package main

import (
	"encoding/json"
	"log"
	"path"
	"time"
)

// FSCKStatus struct
type FSCKStatus struct {
	LastVerifySuccessful bool
	LastBackupSuccessful bool
	BackupCount          int
}

// NeedFSCK 检测是否需要 fsck
// 距离上次 fsck 之后成功 backup 了 n 次
// 或者上次 verify 失败了
// 或者上次 backup 开始更新了网络，但是没完成，没动网络似乎没事
func (f *FSCKStatus) NeedFSCK() bool {
	return false
}

// SetLastVerifySuccessful set flag
func (f *FSCKStatus) SetLastVerifySuccessful() {

}

// SetLastBackupSuccessful set flag
func (f *FSCKStatus) SetLastBackupSuccessful() {

}

// SetBackupCount set count
func (f *FSCKStatus) SetBackupCount() {

}

func fsckRemote(config Config) {
	riPath := path.Join(config.WorkingDir, config.Index+".remote")
	if !downloadRemoteIndex(config, riPath) {
		log.Println("Can't download remote index")
		return
	}
	ri := loadIndex(riPath)

	deleteOutdatedChunks(config, &ri)

	cm := scanRemoteChunksMap(config)

	for fp, fi := range ri.Files {
		if fi.IsDir || fi.LinkTo != "" {
			continue
		}
		ch := chunkPath(fi.Size, fi.Hash)
		_, ok := cm[ch]
		if ok {
			cm[ch] = true
		} else {
			delete(ri.Files, fp)
		}
	}

	for k := range ri.Chunks {
		_, ok := cm[k]
		if ok {
			cm[k] = true
		} else {
			delete(ri.Chunks, k)
		}
	}

	forever := time.Now().AddDate(10, 0, 0).Unix()
	for k, v := range cm {
		if !v {
			log.Println("Lost found chunk", k)
			ri.Chunks[k] = forever
		}
	}

	j, _ := json.MarshalIndent(ri, "", "  ")
	uploadRemoteIndex(config, j)
}
