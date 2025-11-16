package main

import (
	"bytes"
	"context"
	"fmt"
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
		// Verbose 模式：直接输出所有日志
		defaultLogger = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
			Level: slog.LevelDebug,
		}))
	} else {
		// 默认模式：创建自定义 handler
		defaultLogger = slog.New(&CustomHandler{
			infoHandler: slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
				Level: slog.LevelInfo,
			}),
			debugHandler: slog.NewTextHandler(debugCollector, &slog.HandlerOptions{
				Level: slog.LevelDebug,
			}),
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
	os.Exit(1)
}

// Fatalf 记录格式化错误消息并退出程序，替代 log.Fatalf 的行为
func Fatalf(format string, args ...interface{}) {
	slog.Error(fmt.Sprintf(format, args...))
	os.Exit(1)
}

// PrintDebugDetails 在程序结束时打印调试信息
func PrintDebugDetails() {
	if !verboseMode && debugCollector.HasContent() {
		fmt.Println("\n=== Debug Details ===")
		content := debugCollector.GetContent()
		// 清理每行的前导时间戳和级别信息，使输出更简洁
		lines := strings.Split(content, "\n")
		for _, line := range lines {
			if line != "" {
				// 移除时间戳和级别信息，只保留消息内容
				if idx := strings.Index(line, "msg="); idx != -1 {
					fmt.Println("[DEBUG]" + line[idx:])
				} else {
					fmt.Println("[DEBUG]" + line)
				}
			}
		}
	}
}

// CustomHandler 自定义日志处理器
type CustomHandler struct {
	infoHandler  slog.Handler
	debugHandler slog.Handler
}

// Enabled 实现 slog.Handler 接口
func (h *CustomHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= slog.LevelInfo
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