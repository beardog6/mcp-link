package main

import (
	"context"
	"encoding/json"
	"fmt"
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
