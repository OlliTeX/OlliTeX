package ometrics

import "os"

// UvThreadpoolSize mirrors uv_threadpool_size.js: the configured UV threadpool
// size, or the Node libuv default (4).
func UvThreadpoolSize() string {
	if v := os.Getenv("UV_THREADPOOL_SIZE"); v != "" {
		return v
	}
	return "4"
}
