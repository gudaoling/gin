// Copyright 2014 Manu Martinez-Almeida.  All rights reserved.
// Use of this source code is governed by a MIT style
// license that can be found in the LICENSE file.

package gin

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http/httputil"
	"runtime"
)

var (
	dunno     = []byte("???")
	centerDot = []byte("·")
	dot       = []byte(".")
	slash     = []byte("/")
)

//Recovery 会返回一个从任何恐慌中恢复的中间件，如果有的话，http写入状态码500
func Recovery() HandlerFunc {
	return RecoveryWithWriter(DefaultErrorWriter)
}

// RecoveryWithWriter 会返回一个从任何恐慌中恢复的中间件，如果有的话，http写入状态码500.
func RecoveryWithWriter(out io.Writer) HandlerFunc {
	var logger *log.Logger
	if out != nil {
		logger = log.New(out, "\n\n\x1b[31m", log.LstdFlags)
	}
	return func(c *Context) {
		defer func() {
			if err := recover(); err != nil {
				if logger != nil {
					stack := stack(3)
					httprequest, _ := httputil.DumpRequest(c.Request, false)
					logger.Printf("[Recovery] panic recovered:\n%s\n%s\n%s%s", string(httprequest), err, stack, reset)
				}
				c.AbortWithStatus(500)
			}
		}()
		c.Next()
	}
}

// stack 返回一个格式良好的堆栈框架，跳过跳过帧.
func stack(skip int) []byte {
	buf := new(bytes.Buffer) // the returned data
	//当我们循环时，我们打开文件并读取它们。这些变量记录当前的情况
          //加载文件。
	var lines [][]byte
	var lastFile string
	for i := skip; ; i++ { // 跳过预期的帧数
		pc, file, line, ok := runtime.Caller(i)
		if !ok {
			break
		}
		// 至少要打印这么多。如果我们找不到源，它就不会显示.
		fmt.Fprintf(buf, "%s:%d (0x%x)\n", file, line, pc)
		if file != lastFile {
			data, err := ioutil.ReadFile(file)
			if err != nil {
				continue
			}
			lines = bytes.Split(data, []byte{'\n'})
			lastFile = file
		}
		fmt.Fprintf(buf, "\t%s: %s\n", function(pc), source(lines, line))
	}
	return buf.Bytes()
}

// source 在第n行中返回一个空格切割的部分.
func source(lines [][]byte, n int) []byte {
	n-- // 在堆栈跟踪中，行是1索引的但是我们的数组是0-索引的
	if n < 0 || n >= len(lines) {
		return dunno
	}
	return bytes.TrimSpace(lines[n])
}

// function 如果可能的话，返回包含PC的函数的名称.
func function(pc uintptr) []byte {
	fn := runtime.FuncForPC(pc)
	if fn == nil {
		return dunno
	}
	name := []byte(fn.Name())
	//这个名称包含了包的路径名，这是不必要的
	//因为文件名已经包含在内了。另外，它有中心点。
	//这是我们看到的
	//	runtime/debug.*T·ptrmethod
	// and want
	//	*T.ptrmethod
	// Also the package path might contains dot (e.g. code.google.com/...),
	// so first eliminate the path prefix
	if lastslash := bytes.LastIndex(name, slash); lastslash >= 0 {
		name = name[lastslash+1:]
	}
	if period := bytes.Index(name, dot); period >= 0 {
		name = name[period+1:]
	}
	name = bytes.Replace(name, centerDot, dot, -1)
	return name
}
