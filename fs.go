// Copyright 2017 Manu Martinez-Almeida.  All rights reserved.
// Use of this source code is governed by a MIT style
// license that can be found in the LICENSE file.

package gin

import (
	"net/http"
	"os"
)

type onlyfilesFS struct {
	fs http.FileSystem
}

type neuteredReaddirFile struct {
	http.File
}

// Dir 返回一个可以被 http.FileServer()使用的 http.Filesystem . 它是在内部
//  router.Static()使用.
//如果l listDirectory == true, 那么它的工作方式与 http.Dir()相同 否则它会返回
// filesystem 它可以阻止 http.FileServer() 来列出目录文件.
func Dir(root string, listDirectory bool) http.FileSystem {
	fs := http.Dir(root)
	if listDirectory {
		return fs
	}
	return &onlyfilesFS{fs}
}

// 打开符合 http.Filesystem.
func (fs onlyfilesFS) Open(name string) (http.File, error) {
	f, err := fs.fs.Open(name)
	if err != nil {
		return nil, err
	}
	return neuteredReaddirFile{f}, nil
}

// Readdir覆盖 http.File 默认实现.
func (f neuteredReaddirFile) Readdir(count int) ([]os.FileInfo, error) {
	// 这个禁用目录清单
	return nil, nil
}
