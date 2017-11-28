// Copyright 2014 Manu Martinez-Almeida.  All rights reserved.
// Use of this source code is governed by a MIT style
// license that can be found in the LICENSE file.

package gin

import (
	"errors"
	"io"
	"io/ioutil"
	"math"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/gin-contrib/sse"
	"github.com/gin-gonic/gin/binding"
	"github.com/gin-gonic/gin/render"
)

// 常见数据格式 content-Type  MIME.
const (
	MIMEJSON              = binding.MIMEJSON
	MIMEHTML              = binding.MIMEHTML
	MIMEXML               = binding.MIMEXML
	MIMEXML2              = binding.MIMEXML2
	MIMEPlain             = binding.MIMEPlain
	MIMEPOSTForm          = binding.MIMEPOSTForm
	MIMEMultipartPOSTForm = binding.MIMEMultipartPOSTForm
)

const abortIndex int8 = math.MaxInt8 / 2

// 上下文是gin中最重要的部分. 它允许我们在中间件之间传递变量,
// 管理数据流, 验证请求的JSON 和 呈现一个JSON响应.
type Context struct {
	writermem responseWriter
	Request   *http.Request
	Writer    ResponseWriter

	Params   Params
	handlers HandlersChain
	index    int8

	engine *Engine

	// 键是一个键/值对，只适用于每个请求的上下文.
	Keys map[string]interface{}

	// 错误是附加到使用此上下文的所有处理程序/中间件的错误列表.
	Errors errorMsgs

	// 已接受的定义了内容协商的手动接受格式列表.
	Accepted []string
}

/************************************/
/********** 上下文创建 ********/
/************************************/

func (c *Context) reset() {
	c.Writer = &c.writermem
	c.Params = c.Params[0:0]
	c.handlers = nil
	c.index = -1
	c.Keys = nil
	c.Errors = c.Errors[0:0]
	c.Accepted = nil
}

// 复制返回当前上下文的一个副本，可以安全地在请求的范围之外使用.
// 当上下文必须传递到goroutine时，必须使用此方法.
func (c *Context) Copy() *Context {
	var cp = *c
	cp.writermem.ResponseWriter = nil
	cp.Writer = &cp.writermem
	cp.index = abortIndex
	cp.handlers = nil
	return &cp
}

// HandlerName返回主处理程序的名称。例如，如果处理程序是"handleGetUsers()",
// 这个函数将返回 "main.handleGetUsers".
func (c *Context) HandlerName() string {
	return nameOfFunction(c.handlers.Last())
}

// Handler 返回 main handler.
func (c *Context) Handler() HandlerFunc {
	return c.handlers.Last()
}

/************************************/
/*********** 流控制 ***********/
/************************************/

//接下来应该只在中间件内部使用.
// 它在调用处理程序内的链中执行挂起的处理程序（挂起，程序放到后台，程序没有结束）.
// 在GitHub看到例子.
func (c *Context) Next() {
	c.index++
	for s := int8(len(c.handlers)); c.index < s; c.index++ {
		c.handlers[c.index](c)
	}
}

// 如果当前上下文被中止，则IsAborted返回true.
func (c *Context) IsAborted() bool {
	return c.index >= abortIndex
}

// Abort防止挂起的处理程序被调用。注意，这不会停止当前的处理程序.
// 假设您有一个授权中间件，它验证当前请求是否被授权.
// 如果授权失败 (ex: the password does not match),调用Abort确保该请求的剩余处理程序
// 不被调用 .
func (c *Context) Abort() {
	c.index = abortIndex
}

// AbortWithStatus 调用 `Abort()` 和 用指定的状态码写入headers.
// 例如，对请求进行身份验证的失败尝试可以使用: context.AbortWithStatus(401).
func (c *Context) AbortWithStatus(code int) {
	c.Status(code)
	c.Writer.WriteHeaderNow()
	c.Abort()
}

// AbortWithStatusJSON 调用 `Abort()` 并添加 `JSON` .
// 此方法停止链，写入状态代码并返回JSON主体.
// It also sets the Content-Type as "application/json".
func (c *Context) AbortWithStatusJSON(code int, jsonObj interface{}) {
	c.Abort()
	c.JSON(code, jsonObj)
}

// AbortWithError 调用 `AbortWithStatus()` 和 `Error()` .
// 此方法停止链, 写入状态代码并将指定的错误推到 `c.Errors`.
// See Context.Error() for more details.
func (c *Context) AbortWithError(code int, err error) *Error {
	c.AbortWithStatus(code)
	return c.Error(err)
}

/************************************/
/********* 错误管理 *********/
/************************************/

// Error将错误附加到当前上下文. 错误被推到一个错误列表中.
// 对于在请求的解析过程中发生的每个错误，调用错误是一个好主意.
// 一个中间件可以用来收集所有的错误，并将它们推到一个数据库中,
// 打印日志，或者在HTTP响应中附加它.
// 如果err为nil，错误将会引起恐慌.
func (c *Context) Error(err error) *Error {
	if err == nil {
		panic("err is nil")
	}
	var parsedError *Error
	switch err.(type) {
	case *Error:
		parsedError = err.(*Error)
	default:
		parsedError = &Error{
			Err:  err,
			Type: ErrorTypePrivate,
		}
	}
	c.Errors = append(c.Errors, parsedError)
	return parsedError
}

/************************************/
/******** 元数据管理 ********/
/************************************/

// Set用于为这个上下文专门存储一个新的键/值对.
//  如果不是之前使用的键 它还会初始化 c.Keys.
func (c *Context) Set(key string, value interface{}) {
	if c.Keys == nil {
		c.Keys = make(map[string]interface{})
	}
	c.Keys[key] = value
}

// Get返回给定键的值, 即: (value, true).
// 如果值不存在，它就返回 (nil, false)
func (c *Context) Get(key string) (value interface{}, exists bool) {
	value, exists = c.Keys[key]
	return
}

// 如果键存在, MustGet返回它的值, 否则它恐慌.
func (c *Context) MustGet(key string) interface{} {
	if value, exists := c.Get(key); exists {
		return value
	}
	panic("Key \"" + key + "\" does not exist")
}

// GetString 返回与键关联的值作为字符串.
func (c *Context) GetString(key string) (s string) {
	if val, ok := c.Get(key); ok && val != nil {
		s, _ = val.(string)
	}
	return
}

// GetBool 返回与见关联的值作为布尔值.
func (c *Context) GetBool(key string) (b bool) {
	if val, ok := c.Get(key); ok && val != nil {
		b, _ = val.(bool)
	}
	return
}

// GetInt  返回与见关联的值作为整数.
func (c *Context) GetInt(key string) (i int) {
	if val, ok := c.Get(key); ok && val != nil {
		i, _ = val.(int)
	}
	return
}

// GetInt64  返回与见关联的值作为整数.
func (c *Context) GetInt64(key string) (i64 int64) {
	if val, ok := c.Get(key); ok && val != nil {
		i64, _ = val.(int64)
	}
	return
}

// GetFloat64 返回与见关联的值作为float64.
func (c *Context) GetFloat64(key string) (f64 float64) {
	if val, ok := c.Get(key); ok && val != nil {
		f64, _ = val.(float64)
	}
	return
}

// GetTime  返回与见关联的值作为时间.
func (c *Context) GetTime(key string) (t time.Time) {
	if val, ok := c.Get(key); ok && val != nil {
		t, _ = val.(time.Time)
	}
	return
}

// GetDuration 返回与见关联的值作为时长.
func (c *Context) GetDuration(key string) (d time.Duration) {
	if val, ok := c.Get(key); ok && val != nil {
		d, _ = val.(time.Duration)
	}
	return
}

// GetStringSlice 返回与见关联的值作为字符串切片.
func (c *Context) GetStringSlice(key string) (ss []string) {
	if val, ok := c.Get(key); ok && val != nil {
		ss, _ = val.([]string)
	}
	return
}

// GetStringMap 返回与见关联的值作为map of interfaces.
func (c *Context) GetStringMap(key string) (sm map[string]interface{}) {
	if val, ok := c.Get(key); ok && val != nil {
		sm, _ = val.(map[string]interface{})
	}
	return
}

// GetStringMapString 返回与见关联的值作为a map of strings.
func (c *Context) GetStringMapString(key string) (sms map[string]string) {
	if val, ok := c.Get(key); ok && val != nil {
		sms, _ = val.(map[string]string)
	}
	return
}

// GetStringMapStringSlice 返回与见关联的值作为 map to a slice of strings.
func (c *Context) GetStringMapStringSlice(key string) (smss map[string][]string) {
	if val, ok := c.Get(key); ok && val != nil {
		smss, _ = val.(map[string][]string)
	}
	return
}

/************************************/
/************ 输入数据 ************/
/************************************/

// Param返回URL Param的值.
// 它是c.Params.ByName(key)的快捷方式
//     router.GET("/user/:id", func(c *gin.Context) {
//         // a GET request to /user/john
//         id := c.Param("id") // id == "john"
//     })
func (c *Context) Param(key string) string {
	return c.Params.ByName(key)
}

// 查询返回键入url 请求值, 如果它存在的话 ,
// 否则返回一个空的字符串 `("")`.
// 它是 `c.Request.URL.Query().Get(key)`的快捷方式
//     GET /path?id=1234&name=Manu&value=
// 	   c.Query("id") == "1234"
// 	   c.Query("name") == "Manu"
// 	   c.Query("value") == ""
// 	   c.Query("wtf") == ""
func (c *Context) Query(key string) string {
	value, _ := c.GetQuery(key)
	return value
}

// DefaultQuery 返回键入url 请求值, 如果它存在的话,
// 否则返回指定的defaultValue（默认值）字符串.
// 查看: Query() and GetQuery()以获得进一步的信息.
//     GET /?name=Manu&lastname=
//     c.DefaultQuery("name", "unknown") == "Manu"
//     c.DefaultQuery("id", "none") == "none"
//     c.DefaultQuery("lastname", "none") == ""
func (c *Context) DefaultQuery(key, defaultValue string) string {
	if value, ok := c.GetQuery(key); ok {
		return value
	}
	return defaultValue
}

// GetQuery 类似 Query(), 返回键入url 请求值
// `(value, true)`，如果存在 ， (即使值是空字符串),
// 否则返回 `("", false)`.
// 它是 `c.Request.URL.Query().Get(key)`的快捷方式
//     GET /?name=Manu&lastname=
//     ("Manu", true) == c.GetQuery("name")
//     ("", false) == c.GetQuery("id")
//     ("", true) == c.GetQuery("lastname")
func (c *Context) GetQuery(key string) (string, bool) {
	if values, ok := c.GetQueryArray(key); ok {
		return values[0], ok
	}
	return "", false
}

// QueryArray 返回 给定查询键的切片字符串 .
// 切片的长度取决于给定键的params的数量.
func (c *Context) QueryArray(key string) []string {
	values, _ := c.GetQueryArray(key)
	return values
}

//GetQueryArray返回给定查询键的切片字符串，加上
//一个布尔值，对于给定的键是否至少存在一个值.
func (c *Context) GetQueryArray(key string) ([]string, bool) {
	if values, ok := c.Request.URL.Query()[key]; ok && len(values) > 0 {
		return values, true
	}
	return []string{}, false
}

// PostForm 从一个urlencoded 的表单或multipart 表单返回指定键的值，如果存在时。
// 否则返回一个空的字符串 `("")`.
func (c *Context) PostForm(key string) string {
	value, _ := c.GetPostForm(key)
	return value
}

// DefaultPostForm 从一个urlencoded 的表单或multipart 表单返回指定键的值，如果存在时。
//否则返回指定的defaultValue默认值.
// 查看: PostForm() 和GetPostForm() 进一步的信息.
func (c *Context) DefaultPostForm(key, defaultValue string) string {
	if value, ok := c.GetPostForm(key); ok {
		return value
	}
	return defaultValue
}

// GetPostForm 类似 PostForm(key).从一个urlencoded 的表单或multipart 表单返回指定键的`(value, true)`值，如果存在时 。
// (即使当前值是空字符串时),
// 否则返回 ("", false).
// 例如, 在一個PATCH 请求去修改  用户email:
//     email=mail@example.com  -->  ("mail@example.com", true) := GetPostForm("email") // 设置 email to "mail@example.com"
// 	   email=                  -->  ("", true) := GetPostForm("email") // 设置email to ""
//                             -->  ("", false) := GetPostForm("email") // 什么都不做的 email
func (c *Context) GetPostForm(key string) (string, bool) {
	if values, ok := c.GetPostFormArray(key); ok {
		return values[0], ok
	}
	return "", false
}

//PostFormArray为给定的表单键返回一个切片字符串。
//这个切片的长度取决于给定键的params的数量。
func (c *Context) PostFormArray(key string) []string {
	values, _ := c.GetPostFormArray(key)
	return values
}

//GetPostFormArray为给定的表单键返回一个切片字符串。
//返回一个布尔值，对于给定的键是否至少存在一个值。
func (c *Context) GetPostFormArray(key string) ([]string, bool) {
	req := c.Request
	req.ParseForm()
	req.ParseMultipartForm(c.engine.MaxMultipartMemory)
	if values := req.PostForm[key]; len(values) > 0 {
		return values, true
	}
	if req.MultipartForm != nil && req.MultipartForm.File != nil {
		if values := req.MultipartForm.Value[key]; len(values) > 0 {
			return values, true
		}
	}
	return []string{}, false
}

// FormFile返回提供的表单键的第一个文件.
func (c *Context) FormFile(name string) (*multipart.FileHeader, error) {
	_, fh, err := c.Request.FormFile(name)
	return fh, err
}

// MultipartForm  是解析的multipart 表单，包括文件上传 .
func (c *Context) MultipartForm() (*multipart.Form, error) {
	err := c.Request.ParseMultipartForm(c.engine.MaxMultipartMemory)
	return c.Request.MultipartForm, err
}

// SaveUploadedFile 将表单文件上传到特定的dst.
func (c *Context) SaveUploadedFile(file *multipart.FileHeader, dst string) error {
	src, err := file.Open()
	if err != nil {
		return err
	}
	defer src.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	io.Copy(out, src)
	return nil
}

// 绑定检查Content-Type以自动选择绑定引擎,
// 根据the "Content-Type" header 使用不同的绑定 :
//     "application/json" --> JSON binding
//     "application/xml"  --> XML binding
// 否则--> 返回一个错误
//它将请求的body解析为JSON，如果Content-Type == "application/json"使用JSON或XML作为JSON输入。
//它将json有效值解码为指定为指针的结构体。
//如果输入无效，它将在响应中写入400个错误并设置 Content-Type header "text/plain" 。
func (c *Context) Bind(obj interface{}) error {
	b := binding.Default(c.Request.Method, c.ContentType())
	return c.MustBindWith(obj, b)
}

// BindJSON 是 c.MustBindWith(obj, binding.JSON) 快捷方式.
func (c *Context) BindJSON(obj interface{}) error {
	return c.MustBindWith(obj, binding.JSON)
}

// BindQuery 是 c.MustBindWith(obj, binding.Query) 快捷方式.
func (c *Context) BindQuery(obj interface{}) error {
	return c.MustBindWith(obj, binding.Query)
}

//MustBindWith  绑定使用指定的绑定引擎将传递的struct指针绑定在一起。
//如果有任何错误的ocurrs，它将以HTTP 400的请求终止请求。
//看到binding包。
func (c *Context) MustBindWith(obj interface{}, b binding.Binding) (err error) {
	if err = c.ShouldBindWith(obj, b); err != nil {
		c.AbortWithError(400, err).SetType(ErrorTypeBind)
	}

	return
}

//ShouldBind  Content-Type  来自动选择一个绑定引擎，
//根据"Content-Type" header 标题不同的绑定使用:
//     "application/json" --> JSON binding
//     "application/xml"  --> XML binding
// 否则--> 返回一个错误
//它将请求的body 解析为JSON，如果Content-Type == "application/json"使用JSON或XML作为JSON输入。
//它将json有效值解码为指定为指针的结构体。
//类似于c.bind()但是这种方法不会将响应状态代码设置为400，如果json不是有效的就会中止。
func (c *Context) ShouldBind(obj interface{}) error {
	b := binding.Default(c.Request.Method, c.ContentType())
	return c.ShouldBindWith(obj, b)
}

// ShouldBindJSON 是 c.ShouldBindWith(obj, binding.JSON)快捷方式.
func (c *Context) ShouldBindJSON(obj interface{}) error {
	return c.ShouldBindWith(obj, binding.JSON)
}

// ShouldBindQuery 是 c.ShouldBindWith(obj, binding.Query) 快捷方式.
func (c *Context) ShouldBindQuery(obj interface{}) error {
	return c.ShouldBindWith(obj, binding.Query)
}

// ShouldBindWith使用指定的绑定引擎将传递的结构指针绑定在一起.
// 查看binding 包.
func (c *Context) ShouldBindWith(obj interface{}, b binding.Binding) error {
	return b.Bind(c.Request, obj)
}

//ClientIP实现了一个最好的算法来返回真正的客户端IP，它解析
//x-real-ip和x-代理，以便能正确地使用反向代理，如nginx或haproxy。
//在x-real-IP之前使用x-实数，因为nginx使用了x-实数IP和代理的IP。
func (c *Context) ClientIP() string {
	if c.engine.ForwardedByClientIP {
		clientIP := c.requestHeader("X-Forwarded-For")
		if index := strings.IndexByte(clientIP, ','); index >= 0 {
			clientIP = clientIP[0:index]
		}
		clientIP = strings.TrimSpace(clientIP)
		if clientIP != "" {
			return clientIP
		}
		clientIP = strings.TrimSpace(c.requestHeader("X-Real-Ip"))
		if clientIP != "" {
			return clientIP
		}
	}

	if c.engine.AppEngine {
		if addr := c.requestHeader("X-Appengine-Remote-Addr"); addr != "" {
			return addr
		}
	}

	if ip, _, err := net.SplitHostPort(strings.TrimSpace(c.Request.RemoteAddr)); err == nil {
		return ip
	}

	return ""
}

// ContentType 返回请求 Content-Type 的header .
func (c *Context) ContentType() string {
	return filterFlags(c.requestHeader("Content-Type"))
}

//IsWebsocket 返回true，如果请求头指示一个websocket
//handshake（握手）是由客户发起的。
func (c *Context) IsWebsocket() bool {
	if strings.Contains(strings.ToLower(c.requestHeader("Connection")), "upgrade") &&
		strings.ToLower(c.requestHeader("Upgrade")) == "websocket" {
		return true
	}
	return false
}

func (c *Context) requestHeader(key string) string {
	return c.Request.Header.Get(key)
}

/************************************/
/******** 响应渲染 ********/
/************************************/

//bodyAllowedForStatus  是一个http http.bodyAllowedForStatus  non-exported函数的副本
func bodyAllowedForStatus(status int) bool {
	switch {
	case status >= 100 && status <= 199:
		return false
	case status == 204:
		return false
	case status == 304:
		return false
	}
	return true
}

// Status 设置 HTTP 响应码.
func (c *Context) Status(code int) {
	c.writermem.WriteHeader(code)
}

////Header是c.Writer.Header().Set(key, value)的一种智能快捷方式  .
// 它在响应中写入一个header .
// 如果 value == "",header 会删除`c.Writer.Header().Del(key)`
func (c *Context) Header(key, value string) {
	if value == "" {
		c.Writer.Header().Del(key)
	} else {
		c.Writer.Header().Set(key, value)
	}
}

// GetHeader 从请求headers返回值.
func (c *Context) GetHeader(key string) string {
	return c.requestHeader(key)
}

// GetRawData 返回数据流.
func (c *Context) GetRawData() ([]byte, error) {
	return ioutil.ReadAll(c.Request.Body)
}

// SetCookie 添加一个 Set-Cookie header 到ResponseWriter's headers.
//提供的cookie 必须有一个有效的名称。无效的cookies 可能
//默默地下降。
func (c *Context) SetCookie(name, value string, maxAge int, path, domain string, secure, httpOnly bool) {
	if path == "" {
		path = "/"
	}
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     name,
		Value:    url.QueryEscape(value),
		MaxAge:   maxAge,
		Path:     path,
		Domain:   domain,
		Secure:   secure,
		HttpOnly: httpOnly,
	})
}

//Cookie返回请求中提供的指定Cookie
如果找不到的话。返回指定的cookie是不可转义的。
//如果多个cookie与给定的名称匹配，只有一个cookie
/ /返回。
func (c *Context) Cookie(name string) (string, error) {
	cookie, err := c.Request.Cookie(name)
	if err != nil {
		return "", err
	}
	val, _ := url.QueryUnescape(cookie.Value)
	return val, nil
}

func (c *Context) Render(code int, r render.Render) {
	c.Status(code)

	if !bodyAllowedForStatus(code) {
		r.WriteContentType(c.Writer)
		c.Writer.WriteHeaderNow()
		return
	}

	if err := r.Render(c.Writer); err != nil {
		panic(err)
	}
}

//HTML 渲染由其文件名指定的HTTP模板。
//它还更新HTTP代码，并将 Content-Type设置为"text/html"。
// 查看http://golang.org/doc/articles/wiki/

func (c *Context) HTML(code int, name string, obj interface{}) {
	instance := c.engine.HTMLRender.Instance(name, obj)
	c.Render(code, instance)
}

//缩进JSON将给定的结构体序列化为一个漂亮的JSON(缩进的+endlines)到响应体中。
//它还将内容类型设置为"application/json"。
//警告:我们建议只用于开发目的，因为打印漂亮的JSON是
//更多的CPU和带宽消耗。使用Context.JSON()代替。
func (c *Context) IndentedJSON(code int, obj interface{}) {
	c.Render(code, render.IndentedJSON{Data: obj})
}

//将给定的结构体序列化为安全的JSON到响应体中。
//默认预惩罚"while(1),"如果给定的结构是数组值，则响应正文。
//它还将内容类型设置为"application/json"。
func (c *Context) SecureJSON(code int, obj interface{}) {
	c.Render(code, render.SecureJSON{Prefix: c.engine.secureJsonPrefix, Data: obj})
}


//JSON将给定的结构体序列化为JSON到响应体中。
//它还将内容类型设置为"application/json".。
func (c *Context) JSON(code int, obj interface{}) {
	c.Render(code, render.JSON{Data: obj})
}


//XML 将给定的结构体序列化为XML 到响应体中。
//它还将内容类型设置为"application/xml".。
func (c *Context) XML(code int, obj interface{}) {
	c.Render(code, render.XML{Data: obj})
}

//YAML 将给定的结构体序列化为YAML 到响应体中。
func (c *Context) YAML(code int, obj interface{}) {
	c.Render(code, render.YAML{Data: obj})
}

// String 将给定的结构体序列化为字符串 到响应体中.
func (c *Context) String(code int, format string, values ...interface{}) {
	c.Render(code, render.String{Format: format, Data: values})
}

// Redirect 返回一个HTTP 重定向到指定的位置.
func (c *Context) Redirect(code int, location string) {
	c.Render(-1, render.Redirect{
		Code:     code,
		Location: location,
		Request:  c.Request,
	})
}

// Data在正文流中写入一些数据，并更新HTTP代码.
func (c *Context) Data(code int, contentType string, data []byte) {
	c.Render(code, render.Data{
		ContentType: contentType,
		Data:        data,
	})
}

//File 以一种有效的方式将指定的文件写入到正文流中
func (c *Context) File(filepath string) {
	http.ServeFile(c.Writer, c.Request, filepath)
}

// SSEvent 将一个Server-Sent 事件写入到正文流中.
func (c *Context) SSEvent(name string, message interface{}) {
	c.Render(-1, sse.Event{
		Event: name,
		Data:  message,
	})
}

func (c *Context) Stream(step func(w io.Writer) bool) {
	w := c.Writer
	clientGone := w.CloseNotify()
	for {
		select {
		case <-clientGone:
			return
		default:
			keepOpen := step(w)
			w.Flush()
			if !keepOpen {
				return
			}
		}
	}
}

/************************************/
/******** 内容协商 *******/
/************************************/

type Negotiate struct {
	Offered  []string
	HTMLName string
	HTMLData interface{}
	JSONData interface{}
	XMLData  interface{}
	Data     interface{}
}

func (c *Context) Negotiate(code int, config Negotiate) {
	switch c.NegotiateFormat(config.Offered...) {
	case binding.MIMEJSON:
		data := chooseData(config.JSONData, config.Data)
		c.JSON(code, data)

	case binding.MIMEHTML:
		data := chooseData(config.HTMLData, config.Data)
		c.HTML(code, config.HTMLName, data)

	case binding.MIMEXML:
		data := chooseData(config.XMLData, config.Data)
		c.XML(code, data)

	default:
		c.AbortWithError(http.StatusNotAcceptable, errors.New("the accepted formats are not offered by the server"))
	}
}

func (c *Context) NegotiateFormat(offered ...string) string {
	assert1(len(offered) > 0, "you must provide at least one offer")

	if c.Accepted == nil {
		c.Accepted = parseAccept(c.requestHeader("Accept"))
	}
	if len(c.Accepted) == 0 {
		return offered[0]
	}
	for _, accepted := range c.Accepted {
		for _, offert := range offered {
			if accepted == offert {
				return offert
			}
		}
	}
	return ""
}

func (c *Context) SetAccepted(formats ...string) {
	c.Accepted = formats
}

/************************************/
/***** GOLANG.ORG/X/NET/CONTEXT *****/
/************************************/

func (c *Context) Deadline() (deadline time.Time, ok bool) {
	return
}

func (c *Context) Done() <-chan struct{} {
	return nil
}

func (c *Context) Err() error {
	return nil
}

func (c *Context) Value(key interface{}) interface{} {
	if key == 0 {
		return c.Request
	}
	if keyAsString, ok := key.(string); ok {
		val, _ := c.Get(keyAsString)
		return val
	}
	return nil
}
