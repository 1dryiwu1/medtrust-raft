// Package raft — 彩色调试日志工具
package raft

import (
	"fmt"
	"log"
	"time"
)

// ANSI 颜色转义码
const (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorCyan   = "\033[36m"
	colorWhite  = "\033[97m"
	colorBold   = "\033[1m"
)

// tag 颜色映射
var tagColor = map[string]string{
	"ELECTION":  colorYellow,
	"VOTE":      colorCyan,
	"LEADER":    colorGreen + colorBold,
	"HEARTBEAT": colorWhite,
	"REPLICATE": colorCyan,
	"CONSENSUS": colorGreen + colorBold,
	"FOLLOWER":  colorWhite,
	"WARN":      colorRed,
	"BLOCK":     colorGreen,
	"API":       colorCyan,
	"STORE":     colorWhite,
}

// raftLog 打印带时间戳和彩色 tag 的日志行。
//
// 示例输出：
//
//	14:05:23.001 [ELECTION]  Node node-1 started election for Term 2
//	14:05:23.012 [LEADER]    Node node-2 is elected as Leader (Term 2)
func raftLog(tag, nodeID, format string, args ...any) {
	color, ok := tagColor[tag]
	if !ok {
		color = colorWhite
	}
	msg := fmt.Sprintf(format, args...)
	ts := time.Now().Format("15:04:05.000")
	log.Printf("%s %s[%-9s]%s Node %-8s %s",
		ts, color, tag, colorReset, nodeID, msg)
}
