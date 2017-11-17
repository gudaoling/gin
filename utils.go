//版权所有2014 Manu Martinez-Almeida。版权所有。
//使用这个源代码由MIT风格管理
//可以在LICENSE文件中找到许可证。

package gin

import (
	"encoding/xml"
	"net/http"
	"os"
	"path"
	"reflect"
	"runtime"
	"strings"
)

const BindKey = "_gin-gonic/gin/bindkey"

func Bind(val interface{}) HandlerFunc {
	value := reflect.ValueOf(val)
	if value.Kind() == reflect.Ptr {
		panic(`绑定结构不能是一个指针.例如:
	 使用: gin.Bind(Struct{}) 而不是 gin.Bind(&Struct{})
`)
	}
	typ := value.Type()

	return func(c *Context) {
		obj := reflect.New(typ).Interface()
		if c.Bind(obj) == nil {
			c.Set(BindKey, obj)
		}
	}
}

func WrapF(f http.HandlerFunc) HandlerFunc {
	return func(c *Context) {
		f(c.Writer, c.Request)
	}
}

func WrapH(h http.Handler) HandlerFunc {
	return func(c *Context) {
		h.ServeHTTP(c.Writer, c.Request)
	}
}

type H map[string]interface{}

// MarshalXML允许类型H与xml.Marshal一起使用。
func (h H) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
	start.Name = xml.Name{
		Space: "",
		Local: "map",
	}
	if err := e.EncodeToken(start); err != nil {
		return err
	}
	for key, value := range h {
		elem := xml.StartElement{
			Name: xml.Name{Space: "", Local: key},
			Attr: []xml.Attr{},
		}
		if err := e.EncodeElement(value, elem); err != nil {
			return err
		}
	}
	if err := e.EncodeToken(xml.EndElement{Name: start.Name}); err != nil {
		return err
	}
	return nil
}

func assert1(guard bool, text string) {
	if !guard {
		panic(text)
	}
}

func filterFlags(content string) string {
	for i, char := range content {
		if char == ' ' || char == ';' {
			return content[:i]
		}
	}
	return content
}

func chooseData(custom, wildcard interface{}) interface{} {
	if custom == nil {
		if wildcard == nil {
			panic("协商配置无效")
		}
		return wildcard
	}
	return custom
}

func parseAccept(acceptHeader string) []string {
	parts := strings.Split(acceptHeader, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if index := strings.IndexByte(part, ';'); index >= 0 {
			part = part[0:index]
		}
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

func lastChar(str string) uint8 {
	if str == "" {
		panic("字符串的长度不能为0")
	}
	return str[len(str)-1]
}

func nameOfFunction(f interface{}) string {
	return runtime.FuncForPC(reflect.ValueOf(f).Pointer()).Name()
}

func joinPaths(absolutePath, relativePath string) string {
	if relativePath == "" {
		return absolutePath
	}

	finalPath := path.Join(absolutePath, relativePath)
	appendSlash := lastChar(relativePath) == '/' && lastChar(finalPath) != '/'
	if appendSlash {
		return finalPath + "/"
	}
	return finalPath
}

func resolveAddress(addr []string) string {
	switch len(addr) {
	case 0:
		if port := os.Getenv("PORT"); port != "" {
			debugPrint("环境变量PORT=\"%s\"", port)
			return ":" + port
		}
		debugPrint("环境变量PORT未定义，默认使用端口：8080")
		return ":8080"
	case 1:
		return addr[0]
	default:
		panic("太多的参数")
	}
}
