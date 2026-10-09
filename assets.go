// Package assets 通过 go:embed 将 HTML 模板与静态资源打包进单一二进制，
// 使服务在任意工作目录/机器（如 CentOS）都能直接运行，无需随附 templates/、static/ 目录。
package assets

import "embed"

// TemplatesFS 嵌入 templates/ 目录（含 *.html）。
//
//go:embed templates
var TemplatesFS embed.FS

// StaticFS 嵌入 static/ 目录（css/js 等）。
//
//go:embed static
var StaticFS embed.FS
