package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/anyisalin/mcp-openapi-to-mcp-adapter/utils"
	"github.com/urfave/cli/v2"
)

func main() {
	app := &cli.App{
		Name:  "mcp-link",
		Usage: "Convert OpenAPI to MCP compatible endpoints",
		Commands: []*cli.Command{
			{
				Name:  "serve",
				Usage: "Start the MCP Link server",
				Flags: []cli.Flag{
					&cli.IntFlag{
						Name:    "port",
						Aliases: []string{"p"},
						Value:   8080,
						Usage:   "Port to listen on",
					},
					&cli.StringFlag{
						Name:    "host",
						Aliases: []string{"H"},
						Value:   "localhost",
						Usage:   "Host to listen on",
					},
				},
				Action: func(c *cli.Context) error {
					return runServer(c.String("host"), c.Int("port"))
				},
			},
		},
	}

	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}
}

func runServer(host string, port int) error {
	// Create server address
	addr := fmt.Sprintf("%s:%d", host, port)

	// Configure the SSE server
	ss := utils.NewSSEServer()

	// 创建路由器来处理多个端点
	mux := http.NewServeMux()
	mux.Handle("/sse", corsMiddleware(ss))
	mux.Handle("/message", corsMiddleware(ss))
	mux.HandleFunc("/calculator", calculatorHandler)
	mux.HandleFunc("/openapi.yaml", openapiHandler)
	mux.HandleFunc("/swagger", swaggerUIHandler)
	mux.HandleFunc("/", swaggerUIHandler)
	
	server := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	// Channel to listen for interrupt signals
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	// Start server in a goroutine
	go func() {
		fmt.Printf("Starting server on %s\n", addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Error starting server: %v\n", err)
		}
	}()

	// Wait for interrupt signal
	<-stop

	// Create a deadline for graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Shutdown the server
	fmt.Println("Shutting down server...")
	if err := ss.Shutdown(ctx); err != nil {
		log.Fatalf("Error shutting down SSE server: %v\n", err)
	}

	if err := server.Shutdown(ctx); err != nil {
		log.Fatalf("Error shutting down HTTP server: %v\n", err)
	}

	fmt.Println("Server gracefully stopped")
	return nil
}

// corsMiddleware adds CORS headers to allow requests from any origin
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Set CORS headers
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "*")
		w.Header().Set("Access-Control-Allow-Headers", "*")

		// Handle preflight requests
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		// Pass the request to the next handler
		next.ServeHTTP(w, r)
	})
}

// CalculatorRequest 定义计算器请求结构
type CalculatorRequest struct {
	Operand1 float64 `json:"operand1"`
	Operand2 float64 `json:"operand2"`
	Operator string  `json:"operator"`
}

// CalculatorResponse 定义计算器响应结构
type CalculatorResponse struct {
	Result float64 `json:"result"`
	Error  string  `json:"error,omitempty"`
}

// calculatorHandler 处理计算器请求
func calculatorHandler(w http.ResponseWriter, r *http.Request) {
	// 只处理POST请求
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 设置响应头
	w.Header().Set("Content-Type", "application/json")

	// 解析请求体
	var req CalculatorRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(CalculatorResponse{
			Error: "Invalid request format",
		})
		return
	}

	// 验证操作符
	validOperators := map[string]bool{
		"+": true,
		"-": true,
		"*": true,
		"/": true,
	}

	if !validOperators[req.Operator] {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(CalculatorResponse{
			Error: "Invalid operator. Use +, -, *, or /",
		})
		return
	}

	// 执行计算
	var result float64

	switch req.Operator {
	case "+":
		result = req.Operand1 + req.Operand2
	case "-":
		result = req.Operand1 - req.Operand2
	case "*":
		result = req.Operand1 * req.Operand2
	case "/":
		if req.Operand2 == 0 {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(CalculatorResponse{
				Error: "Division by zero is not allowed",
			})
			return
		}
		result = req.Operand1 / req.Operand2
	}

	// 返回结果
	response := CalculatorResponse{
		Result: result,
	}

	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("Error encoding response: %v", err)
	}
}

// openapiHandler 提供OpenAPI规范文件
func openapiHandler(w http.ResponseWriter, r *http.Request) {
	// 读取OpenAPI规范文件
	content, err := ioutil.ReadFile("openapi.yaml")
	if err != nil {
		http.Error(w, "OpenAPI specification file not found", http.StatusNotFound)
		return
	}

	// 设置响应头
	w.Header().Set("Content-Type", "text/yaml; charset=utf-8")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	
	// 返回文件内容
	w.Write(content)
}

// swaggerUIHandler 提供Swagger UI界面
func swaggerUIHandler(w http.ResponseWriter, r *http.Request) {
	// 如果请求的是根路径且不是API文档请求，重定向到swagger
	if r.URL.Path == "/" && r.URL.RawQuery == "" {
		http.Redirect(w, r, "/swagger?url=/openapi.yaml", http.StatusFound)
		return
	}

	// 设置响应头
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// Swagger UI HTML
	html := `<!DOCTYPE html>
<html lang="zh-CN">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>MCP Link Calculator API - Swagger UI</title>
    <link rel="stylesheet" type="text/css" href="https://unpkg.com/swagger-ui-dist@5.11.0/swagger-ui.css">
    <style>
        html {
            box-sizing: border-box;
            overflow: -moz-scrollbars-vertical;
            overflow-y: scroll;
        }
        *, *:before, *:after {
            box-sizing: inherit;
        }
        body {
            margin: 0;
            background: #fafafa;
        }
        .topbar {
            background-color: #1a1a1a;
            padding: 10px 20px;
        }
        .topbar .title {
            color: white;
            font-size: 18px;
            font-weight: bold;
        }
        .topbar .subtitle {
            color: #ccc;
            font-size: 14px;
            margin-top: 5px;
        }
    </style>
</head>
<body>
    <div class="topbar">
        <div class="title">MCP Link Calculator API</div>
        <div class="subtitle">计算器工具接口文档</div>
    </div>
    <div id="swagger-ui"></div>
    <script src="https://unpkg.com/swagger-ui-dist@5.11.0/swagger-ui-bundle.js"></script>
    <script src="https://unpkg.com/swagger-ui-dist@5.11.0/swagger-ui-standalone-preset.js"></script>
    <script>
        window.onload = function() {
            const urlParams = new URLSearchParams(window.location.search);
            const specUrl = urlParams.get('url') || '/openapi.yaml';
            
            const ui = SwaggerUIBundle({
                url: specUrl,
                dom_id: '#swagger-ui',
                deepLinking: true,
                presets: [
                    SwaggerUIBundle.presets.apis,
                    SwaggerUIStandalonePreset
                ],
                plugins: [
                    SwaggerUIBundle.plugins.DownloadUrl
                ],
                layout: "StandaloneLayout",
                validatorUrl: null,
                defaultModelsExpandDepth: -1,
                defaultModelExpandDepth: -1,
                displayRequestDuration: true,
                docExpansion: "none",
                filter: true,
                showExtensions: true,
                showCommonExtensions: true,
                supportedSubmitMethods: ['get', 'post', 'put', 'delete', 'patch'],
                persistAuthorization: false,
                tryItOutEnabled: true
            });
            
            // 添加中文语言支持
            ui.initOAuth({
                clientId: "your-client-id",
                clientSecret: "your-client-secret",
                realm: "your-realms",
                appName: "MCP Link Calculator API",
                scopeSeparator: " ",
                additionalQueryStringParams: {}
            });
        };
    </script>
</body>
</html>`
	
	w.Write([]byte(html))
}
