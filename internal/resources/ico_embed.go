package resources

import (
	"embed"
	"io/fs"
)

//go:embed ico
var icoFS embed.FS

// GetIcoFS 返回嵌入的 ico 文件系统
func GetIcoFS() fs.FS {
	return icoFS
}
