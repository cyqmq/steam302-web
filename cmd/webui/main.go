package main

import (
	"flag"
	"fmt"
	"os"

	"steam302-web/internal/rules"
	"steam302-web/internal/webui"
)

func main() {
	root := flag.String("root", "", "项目根目录（默认从 CWD 向上查找）")
	addr := flag.String("addr", "127.0.0.1:34902", "监听地址")
	token := flag.String("token", "", "访问令牌；非回环监听时必填")
	flag.Parse()

	rootDir := *root
	if rootDir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			fatal("%v", err)
		}
		rootDir = rules.FindRoot(cwd)
	}
	if rootDir == "" {
		fatal("未找到 config/rules 目录（请从项目目录运行或用 --root 指定）")
	}

	srv := &webui.Server{Root: rootDir, Token: *token}
	if err := srv.Listen(*addr); err != nil {
		fatal("%v", err)
	}
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "webui: "+format+"\n", args...)
	os.Exit(1)
}
