// catrust 管理本机对 steam302-web 根证书的系统信任：
//
//	install  -> 复制 ca.pem 到系统信任库并 update-ca-certificates
//	remove   -> 删除系统信任库中的该证书并刷新
//	status   -> 报告信任库中是否存在该 CA（需 root 时可写判断）
//
// 需要 root（systemd 单元或 sudo）。被删除/安装的都是本程序自己的 CA，不影响其他信任。
package main

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"steam302-web/internal/rules"
)

const (
	storeDir = "/usr/local/share/ca-certificates"
	storeF   = "steam302-web-ca.crt"
)

func main() {
	root := flag.String("root", "", "项目根目录（默认从 CWD 向上查找）")
	flag.Parse()
	action := flag.Arg(0)
	rootDir := *root
	if rootDir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			fatal("%v", err)
		}
		rootDir = rules.FindRoot(cwd)
	}
	if rootDir == "" {
		fatal("未找到 config/rules（请用 --root 指定）")
	}
	ca := filepath.Join(rootDir, "config", "certs", "ca.pem")
	if _, err := os.Stat(ca); err != nil {
		fatal("缺少根证书 %s: %v", ca, err)
	}
	dst := filepath.Join(storeDir, storeF)

	switch action {
	case "install":
		if err := refresh(false); err != nil {
			fmt.Println("注意: update-ca-certificates 失败（继续以拷贝判断）:", err)
		}
		if err := copyFile(ca, dst); err != nil {
			fatal("写入 %s: %v", dst, err)
		}
		fmt.Printf("已拷贝到 %s\n", dst)
		if err := refresh(true); err != nil {
			fatal("update-ca-certificates: %v", err)
		}
		installed, _ := trusted(ca)
		if installed {
			fmt.Println("已加入系统信任库（update-ca-certificates 成功）。")
		} else {
			fmt.Println("已拷贝，但校验未确认入链（可能被去重，如已存在同名 CA）。")
		}
	case "remove":
		if err := os.Remove(dst); err != nil {
			fmt.Println("信任文件不存在，无需移除:", err)
		} else {
			fmt.Println("已移除", dst)
		}
		_ = refresh(true)
	case "status":
		installed, err := trusted(ca)
		if err != nil {
			fatal("%v", err)
		}
		if installed {
			fmt.Println("已信任")
		} else {
			fmt.Println("未信任")
		}
	case "":
		fatal("用法: catrust <install|remove|status> [--root DIR]")
	default:
		fatal("未知操作: %s（支持 install|remove|status）", action)
	}
}

// trusted 判断 ca.pem 的 SHA-256 指纹是否出现在整合后的系统信任 bundle 中
// （update-ca-certificates 会把 store 证书合并进 /etc/ssl/certs/ca-certificates.crt）。
func trusted(ca string) (bool, error) {
	bundle := "/etc/ssl/certs/ca-certificates.crt"
	data, err := os.ReadFile(bundle)
	if err != nil {
		return false, err
	}
	b, err := os.ReadFile(ca)
	if err != nil {
		return false, err
	}
	block, _ := pem.Decode(b)
	if block == nil {
		return false, fmt.Errorf("ca.pem 不是有效 PEM")
	}
	sum := sha256.Sum256(block.Bytes)
	want := hex.EncodeToString(sum[:])
	for len(data) > 0 {
		var certPEM []byte
		block, rest := pem.Decode(data)
		if block == nil {
			break
		}
		if block.Type == "CERTIFICATE" {
			certPEM = block.Bytes
			if cert, err := x509.ParseCertificate(certPEM); err == nil {
				s := sha256.Sum256(cert.Raw)
				if hex.EncodeToString(s[:]) == want {
					return true, nil
				}
			}
		}
		data = rest
	}
	return false, nil
}

func refresh(afterCopy bool) error {
	cmd := exec.Command("update-ca-certificates", "--fresh")
	out, err := cmd.CombinedOutput()
	if err != nil {
		if !afterCopy {
			// 前置刷新失败不致命
			return err
		}
		return fmt.Errorf("%v\n%s", err, out)
	}
	return nil
}

func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "catrust: "+format+"\n", args...)
	os.Exit(1)
}
