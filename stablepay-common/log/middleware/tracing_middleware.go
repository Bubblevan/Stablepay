package middleware

import (
	"github.com/cloudwego/kitex/client"
	"github.com/cloudwego/kitex/pkg/transmeta"
	"github.com/cloudwego/kitex/server"
	"github.com/kitex-contrib/obs-opentelemetry/tracing"
)



// GetKitexClientOptions 返回支持trace传递的Kitex客户端配置
// 应用层在创建Kitex客户端时应该使用这个函数获取标准配置
//
// 使用示例:
//
//	import "code.wenfu.cn/stablepay/stablepay-common/log/middleware"
//
//	client, err := xxxservice.NewClient(
//	    "service-name",
//	    client.WithHostPorts("127.0.0.1:8888"),
//	    middleware.GetKitexClientOptions()...,
//	)
//
// 注意：此函数现在使用 kitex-contrib/obs-opentelemetry 集成
// 自动实现 OpenTelemetry trace context 在 RPC 调用中的传播
// 客户端会自动从 OpenTelemetry context 中提取 trace 信息并注入到 RPC 元数据
func GetKitexClientOptions() []client.Option {
	return []client.Option{
		// OpenTelemetry 客户端 suite，自动处理 trace 传播
		// 包括：从 context 提取 trace → 注入到 RPC 元数据
		client.WithSuite(tracing.NewClientSuite()),
		// 保留 TTHeader metainfo handler 以支持自定义元数据传递
		client.WithMetaHandler(transmeta.ClientTTHeaderHandler),
	}
}

// GetKitexServerOptions 返回支持trace接收的Kitex服务端配置
// 应用层在创建Kitex服务端时应该使用这个函数获取标准配置
//
// 使用示例:
//
//	import "code.wenfu.cn/stablepay/stablepay-common/log/middleware"
//
//	svr := xxxservice.NewServer(
//	    handler,
//	    server.WithServiceAddr(&net.TCPAddr{IP: net.IPv4zero, Port: 8888}),
//	    middleware.GetKitexServerOptions()...,
//	)
//
// 注意：此函数现在使用 kitex-contrib/obs-opentelemetry 集成
// 自动实现 OpenTelemetry trace context 在 RPC 调用中的接收和传播
// 服务端会自动从 RPC 元数据中提取 trace 信息并恢复到 OpenTelemetry context
func GetKitexServerOptions() []server.Option {
	return []server.Option{
		// OpenTelemetry 服务端 suite，自动处理 trace 接收
		// 包括：从 RPC 元数据提取 trace → 恢复到 context
		server.WithSuite(tracing.NewServerSuite()),
		// 保留 TTHeader metainfo handler 以支持自定义元数据接收
		server.WithMetaHandler(transmeta.ServerTTHeaderHandler),
	}
}
