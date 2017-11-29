// Copyright 2014 Manu Martinez-Almeida.  All rights reserved.
// Use of this source code is governed by a MIT style
// license that can be found in the LICENSE file.

package gin

import (
	"io"
	"os"

	"github.com/gin-gonic/gin/binding"
)

const ENV_GIN_MODE = "GIN_MODE"

const (
	DebugMode   string = "debug"
	ReleaseMode string = "release"
	TestMode    string = "test"
)
const (
	debugCode = iota
	releaseCode
	testCode
)

// DefaultWriter是默认io.Writer值, 使用Gin进行调试输出 和
// 中间件 就像 Logger() 或 Recovery().
// 注意  Logger 和 Recovery 都提供了自定义方式来配置它们
//  输出io.Writer.
// 支持在Windows中使用颜色:
// 		import "github.com/mattn/go-colorable"
// 		gin.DefaultWriter = colorable.NewColorableStdout()
var DefaultWriter io.Writer = os.Stdout
var DefaultErrorWriter io.Writer = os.Stderr

var ginMode = debugCode
var modeName = DebugMode

func init() {
	mode := os.Getenv(ENV_GIN_MODE)
	if mode == "" {
		SetMode(DebugMode)
	} else {
		SetMode(mode)
	}
}

func SetMode(value string) {
	switch value {
	case DebugMode:
		ginMode = debugCode
	case ReleaseMode:
		ginMode = releaseCode
	case TestMode:
		ginMode = testCode
	default:
		panic("gin mode unknown: " + value)
	}
	modeName = value
}

func DisableBindValidation() {
	binding.Validator = nil
}

func EnableJsonDecoderUseNumber() {
	binding.EnableDecoderUseNumber = true
}

func Mode() string {
	return modeName
}
