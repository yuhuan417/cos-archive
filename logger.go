package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
)

// DebugCollector 收集调试信息
type DebugCollector struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

// NewDebugCollector 创建新的调试信息收集器
func NewDebugCollector() *DebugCollector {
	return &DebugCollector{}
}

// Write 实现 io.Writer 接口
func (dc *DebugCollector) Write(p []byte) (n int, err error) {
	dc.mu.Lock()
	defer dc.mu.Unlock()
	return dc.buffer.Write(p)
}

// GetContent 获取收集到的内容
func (dc *DebugCollector) GetContent() string {
	dc.mu.Lock()
	defer dc.mu.Unlock()
	return dc.buffer.String()
}

// HasContent 检查是否有收集到内容
func (dc *DebugCollector) HasContent() bool {
	dc.mu.Lock()
	defer dc.mu.Unlock()
	return dc.buffer.Len() > 0
}

// Clear 清空收集的内容
func (dc *DebugCollector) Clear() {
	dc.mu.Lock()
	defer dc.mu.Unlock()
	dc.buffer.Reset()
}

// 全局变量
var (
	debugCollector *DebugCollector
	verboseMode    bool
	defaultLogger  *slog.Logger
)

// InitLogger 初始化日志系统
func InitLogger(verbose bool) {
	verboseMode = verbose
	debugCollector = NewDebugCollector()

	if verbose {
		// Verbose 模式：直接输出所有日志，使用简洁格式
		defaultLogger = slog.New(&SimpleHandler{
			writer: os.Stdout,
		})
	} else {
		// 默认模式：创建自定义 handler，使用简洁格式
		defaultLogger = slog.New(&CustomHandler{
			infoHandler: &SimpleHandler{
				writer: os.Stdout,
			},
			debugHandler: &SimpleHandler{
				writer: debugCollector,
			},
		})
	}
	
	// 设置为默认 logger
	slog.SetDefault(defaultLogger)
}

// Fatal 记录错误消息并退出程序，替代 log.Fatal 的行为
func Fatal(args ...interface{}) {
	if len(args) > 0 {
		if msg, ok := args[0].(string); ok && len(args) == 1 {
			slog.Error(msg)
		} else {
			// 将参数转换为字符串并连接
			var strArgs []string
			for _, arg := range args {
				strArgs = append(strArgs, fmt.Sprint(arg))
			}
			slog.Error(strings.Join(strArgs, " "))
		}
	}
	// 在退出前打印调试信息
	PrintDebugDetails()
	os.Exit(1)
}

// Fatalf 记录格式化错误消息并退出程序，替代 log.Fatalf 的行为
func Fatalf(format string, args ...interface{}) {
	slog.Error(fmt.Sprintf(format, args...))
	// 在退出前打印调试信息
	PrintDebugDetails()
	os.Exit(1)
}

// PrintDebugDetails 在程序结束时打印调试信息
func PrintDebugDetails() {
	if !verboseMode && debugCollector.HasContent() {
		fmt.Println("\n=== Debug Details ===")
		content := debugCollector.GetContent()
		// 直接输出收集到的 DEBUG 日志，因为 SimpleHandler 已经使用了简洁格式
		fmt.Print(content)
	}
}

// SimpleHandler 简洁的日志处理器，输出格式：level=msg
type SimpleHandler struct {
	writer io.Writer
}

// Enabled 实现 slog.Handler 接口
func (h *SimpleHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= slog.LevelDebug
}

// Handle 实现 slog.Handler 接口，简化输出格式
func (h *SimpleHandler) Handle(ctx context.Context, r slog.Record) error {
	var level string
	switch r.Level {
	case slog.LevelDebug:
		level = "D"
	case slog.LevelInfo:
		level = "I"
	case slog.LevelWarn:
		level = "W"
	case slog.LevelError:
		level = "E"
	default:
		level = "?"
	}
	
	// 获取简短时间格式 (HH:MM:SS)
	t := r.Time
	timeStr := t.Format("15:04:05")
	
	// 输出格式：HH:MM:SS L msg
	fmt.Fprintf(h.writer, "%s %s %s", timeStr, level, r.Message)
	
	// 添加键值对
	r.Attrs(func(a slog.Attr) bool {
		fmt.Fprintf(h.writer, " %s=%v", a.Key, a.Value)
		return true
	})
	
	fmt.Fprintln(h.writer)
	return nil
}

// WithAttrs 实现 slog.Handler 接口
func (h *SimpleHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return h // 简化实现，不处理属性
}

// WithGroup 实现 slog.Handler 接口
func (h *SimpleHandler) WithGroup(name string) slog.Handler {
	return h // 简化实现，不处理分组
}

// CustomHandler 自定义日志处理器
type CustomHandler struct {
	infoHandler  slog.Handler
	debugHandler slog.Handler
}

// Enabled 实现 slog.Handler 接口
func (h *CustomHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return true // 允许所有级别的日志进入 Handle 方法进行分发
}

// Handle 实现 slog.Handler 接口
func (h *CustomHandler) Handle(ctx context.Context, r slog.Record) error {
	if r.Level >= slog.LevelInfo {
		return h.infoHandler.Handle(ctx, r)
	}
	return h.debugHandler.Handle(ctx, r)
}

// WithAttrs 实现 slog.Handler 接口
func (h *CustomHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &CustomHandler{
		infoHandler:  h.infoHandler.WithAttrs(attrs),
		debugHandler: h.debugHandler.WithAttrs(attrs),
	}
}

// WithGroup 实现 slog.Handler 接口
func (h *CustomHandler) WithGroup(name string) slog.Handler {
	return &CustomHandler{
		infoHandler:  h.infoHandler.WithGroup(name),
		debugHandler: h.debugHandler.WithGroup(name),
	}
}