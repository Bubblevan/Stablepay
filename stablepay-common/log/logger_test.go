package log

import (
	"context"
	"testing"
)

func TestSimpleLogging(t *testing.T) {
	// 测试系统级日志 - 已废弃，现在统一使用 Logger
	t.Skip("SimpleInfo/SimpleError/SimpleWarn/SimpleDebug 已废弃，请使用 Logger")
}

func TestTraceLogging(t *testing.T) {
	// 测试业务级日志 - TraceInfo/TraceError 等已废弃，现在统一使用 Logger
	t.Skip("TraceInfo/TraceError/TraceWarn/TraceDebug 已废弃，请使用 Logger")
}

func TestContextFunctions(t *testing.T) {
	ctx := context.Background()

	// 测试设置和获取函数（只测试我们自定义的字段）
	ctx = WithRequestID(ctx, "req_001")
	ctx = WithUserID(ctx, "user_12345")

	// 验证设置的值
	if requestID := GetRequestID(ctx); requestID != "req_001" {
		t.Errorf("期望 request_id='req_001', 实际='%s'", requestID)
	}

	if userID := GetUserID(ctx); userID != "user_12345" {
		t.Errorf("期望 user_id='user_12345', 实际='%s'", userID)
	}
}

func TestLoggerCreation(t *testing.T) {
	// 测试日志器创建
	config := NewConfig("test-service", "1.0.0").
		WithLogging("info", "json", "stdout")

	logger, err := NewLogger(config)
	if err != nil {
		t.Fatalf("创建日志器失败: %v", err)
	}

	if logger == nil {
		t.Fatal("日志器不应为nil")
	}

	// 测试日志器功能
	ctx := context.Background()
	ctx = WithRequestID(ctx, "test_req")

	logger.Info(ctx, "测试日志器", map[string]interface{}{
		"test": "value",
	})
}

func TestTraceConsistencyWithLogger(t *testing.T) {
	// 初始化配置
	config := NewConfig("test-service", "1.0.0").WithTracing("jaeger")
	logger, err := NewLogger(config)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	// 模拟业务代码：在RPC入口处提取trace信息
	ctx := context.Background()

	// 模拟ExtractTraceFromRPCRequest的行为，设置trace信息到context中
	ctx = context.WithValue(ctx, TraceIDKey, "fixed_trace_1234567890abcdef")
	ctx = context.WithValue(ctx, SpanIDKey, "fixed_span_abcdef1234567890")

	t.Log("=== 测试同一个方法内多次日志记录的trace一致性 ===")

	// 第一次日志记录
	t.Log("1. 第一次日志记录：")
	logger.Info(ctx, "Creating Shopify payment session", map[string]interface{}{
		"currency":    "USD",
		"merchant_id": "plus-2-2",
		"order_id":    "11698228134255",
		"amount":      "10",
	})

	// 第二次日志记录（同一个方法内）
	t.Log("2. 第二次日志记录（同一个方法内）：")
	logger.Info(ctx, "No existing session found, creating new payment session and order", map[string]interface{}{
		"order_id":    "11698228134255",
		"merchant_id": "plus-2-2",
	})

	// 第三次日志记录（同一个方法内）
	t.Log("3. 第三次日志记录（同一个方法内）：")
	logger.Info(ctx, "Processing payment request", map[string]interface{}{
		"payment_method": "credit_card",
		"amount":         "10.00",
	})

	// 验证trace信息是否一致
	traceInfo := GetTraceContext(ctx)
	t.Logf("=== 验证结果 ===")
	t.Logf("Trace ID: %s", traceInfo.TraceID)
	t.Logf("Span ID: %s", traceInfo.SpanID)
	t.Logf("Valid: %t", traceInfo.Valid)

	// 检查trace ID是否一致
	if traceInfo.TraceID != "fixed_trace_1234567890abcdef" {
		t.Errorf("Expected trace ID to be 'fixed_trace_1234567890abcdef', got %s", traceInfo.TraceID)
	}

	if traceInfo.SpanID != "fixed_span_abcdef1234567890" {
		t.Errorf("Expected span ID to be 'fixed_span_abcdef1234567890', got %s", traceInfo.SpanID)
	}

	t.Log("✅ 测试通过：同一个方法内的多次日志记录使用相同的trace ID")
}

func TestRealTraceGeneration(t *testing.T) {
	// 初始化配置
	config := NewConfig("test-service", "1.0.0").WithTracing("jaeger")
	logger, err := NewLogger(config)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	t.Log("=== 测试真实的trace ID生成 ===")

	// 模拟真实的业务场景：使用ExtractTraceFromRPCRequest提取trace信息
	ctx := context.Background()

	// 模拟RPC请求，没有预设的trace信息
	ctx = ExtractTraceFromRPCRequest(ctx, nil)

	// 第一次日志记录（会生成新的trace ID）
	t.Log("1. 第一次日志记录（会生成新的trace ID）：")
	logger.Info(ctx, "Creating Shopify payment session", map[string]interface{}{
		"currency":    "USD",
		"merchant_id": "plus-2-2",
		"order_id":    "11698228134255",
		"amount":      "10",
	})

	// 获取第一次的trace信息
	traceInfo1 := GetTraceContext(ctx)
	t.Logf("第一次生成的 Trace ID: %s", traceInfo1.TraceID)
	t.Logf("第一次生成的 Span ID: %s", traceInfo1.SpanID)

	// 第二次日志记录（应该使用相同的trace ID）
	t.Log("2. 第二次日志记录（应该使用相同的trace ID）：")
	logger.Info(ctx, "No existing session found, creating new payment session and order", map[string]interface{}{
		"order_id":    "11698228134255",
		"merchant_id": "plus-2-2",
	})

	// 获取第二次的trace信息
	traceInfo2 := GetTraceContext(ctx)
	t.Logf("第二次获取的 Trace ID: %s", traceInfo2.TraceID)
	t.Logf("第二次获取的 Span ID: %s", traceInfo2.SpanID)

	// 检查trace ID是否一致
	if traceInfo1.TraceID != traceInfo2.TraceID {
		t.Errorf("Trace ID should be consistent, got %s and %s", traceInfo1.TraceID, traceInfo2.TraceID)
	}

	if traceInfo1.SpanID != traceInfo2.SpanID {
		t.Errorf("Span ID should be consistent, got %s and %s", traceInfo1.SpanID, traceInfo2.SpanID)
	}

	t.Log("✅ 测试通过：真实生成的trace ID在同一个方法内保持一致")
}

func TestTraceFromEmptyHeaders(t *testing.T) {
	// 初始化配置
	config := NewConfig("test-service", "1.0.0").WithTracing("jaeger")
	logger, err := NewLogger(config)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	t.Log("=== 测试从空请求头中生成trace ID并保持一致性 ===")

	// 模拟真实的业务场景：请求头中没有trace信息
	ctx := context.Background()

	// 模拟RPC请求，请求头为空（没有trace信息）
	ctx = ExtractTraceFromRPCRequest(ctx, nil)

	t.Log("1. 第一次日志记录（会生成新的trace ID）：")
	logger.Info(ctx, "Creating Shopify payment session", map[string]interface{}{
		"currency":    "USD",
		"merchant_id": "plus-2-2",
		"order_id":    "11698228134255",
		"amount":      "10",
	})

	// 获取第一次的trace信息
	traceInfo1 := GetTraceContext(ctx)
	t.Logf("第一次生成的 Trace ID: %s", traceInfo1.TraceID)
	t.Logf("第一次生成的 Span ID: %s", traceInfo1.SpanID)

	t.Log("2. 第二次日志记录（应该使用相同的trace ID）：")
	logger.Info(ctx, "No existing session found, creating new payment session and order", map[string]interface{}{
		"order_id":    "11698228134255",
		"merchant_id": "plus-2-2",
	})

	// 获取第二次的trace信息
	traceInfo2 := GetTraceContext(ctx)
	t.Logf("第二次获取的 Trace ID: %s", traceInfo2.TraceID)
	t.Logf("第二次获取的 Span ID: %s", traceInfo2.SpanID)

	t.Log("3. 第三次日志记录（应该使用相同的trace ID）：")
	logger.Info(ctx, "Processing payment request", map[string]interface{}{
		"payment_method": "credit_card",
		"amount":         "10.00",
	})

	// 获取第三次的trace信息
	traceInfo3 := GetTraceContext(ctx)
	t.Logf("第三次获取的 Trace ID: %s", traceInfo3.TraceID)
	t.Logf("第三次获取的 Span ID: %s", traceInfo3.SpanID)

	t.Log("4. 第四次日志记录（应该使用相同的trace ID）：")
	logger.Error(ctx, "Payment processing failed", map[string]interface{}{
		"error_code": "PAYMENT_FAILED",
		"reason":     "Insufficient funds",
	})

	// 获取第四次的trace信息
	traceInfo4 := GetTraceContext(ctx)
	t.Logf("第四次获取的 Trace ID: %s", traceInfo4.TraceID)
	t.Logf("第四次获取的 Span ID: %s", traceInfo4.SpanID)

	// 检查所有trace ID是否一致
	allTraceIDs := []string{traceInfo1.TraceID, traceInfo2.TraceID, traceInfo3.TraceID, traceInfo4.TraceID}
	allSpanIDs := []string{traceInfo1.SpanID, traceInfo2.SpanID, traceInfo3.SpanID, traceInfo4.SpanID}

	// 验证所有trace ID都相同
	for i := 1; i < len(allTraceIDs); i++ {
		if allTraceIDs[0] != allTraceIDs[i] {
			t.Errorf("Trace ID should be consistent, got %s and %s", allTraceIDs[0], allTraceIDs[i])
		}
	}

	// 验证所有span ID都相同
	for i := 1; i < len(allSpanIDs); i++ {
		if allSpanIDs[0] != allSpanIDs[i] {
			t.Errorf("Span ID should be consistent, got %s and %s", allSpanIDs[0], allSpanIDs[i])
		}
	}

	t.Logf("=== 验证结果 ===")
	t.Logf("所有日志使用相同的 Trace ID: %s", allTraceIDs[0])
	t.Logf("所有日志使用相同的 Span ID: %s", allSpanIDs[0])
	t.Log("✅ 测试通过：从空请求头生成的trace ID在多次日志记录中保持一致")
}

func TestTraceAcrossDifferentMethods(t *testing.T) {
	// 初始化配置
	config := NewConfig("test-service", "1.0.0").WithTracing("jaeger")
	logger, err := NewLogger(config)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	t.Log("=== 测试不同方法间的trace ID一致性 ===")

	// 模拟真实的业务场景：请求头中没有trace信息
	ctx := context.Background()

	// 在入口处提取trace信息
	ctx = ExtractTraceFromRPCRequest(ctx, nil)

	// 模拟第一个方法：创建支付会话
	t.Log("=== 方法1：创建支付会话 ===")
	logger.Info(ctx, "Creating Shopify payment session", map[string]interface{}{
		"currency":    "USD",
		"merchant_id": "plus-2-2",
		"order_id":    "11698228134255",
		"amount":      "10",
	})

	traceInfo1 := GetTraceContext(ctx)
	t.Logf("方法1的 Trace ID: %s", traceInfo1.TraceID)
	t.Logf("方法1的 Span ID: %s", traceInfo1.SpanID)

	// 模拟第二个方法：处理支付请求
	t.Log("=== 方法2：处理支付请求 ===")
	logger.Info(ctx, "Processing payment request", map[string]interface{}{
		"payment_method": "credit_card",
		"amount":         "10.00",
		"order_id":       "11698228134255",
	})

	traceInfo2 := GetTraceContext(ctx)
	t.Logf("方法2的 Trace ID: %s", traceInfo2.TraceID)
	t.Logf("方法2的 Span ID: %s", traceInfo2.SpanID)

	// 模拟第三个方法：验证支付
	t.Log("=== 方法3：验证支付 ===")
	logger.Info(ctx, "Validating payment", map[string]interface{}{
		"card_number": "****1234",
		"cvv":         "***",
		"order_id":    "11698228134255",
	})

	traceInfo3 := GetTraceContext(ctx)
	t.Logf("方法3的 Trace ID: %s", traceInfo3.TraceID)
	t.Logf("方法3的 Span ID: %s", traceInfo3.SpanID)

	// 模拟第四个方法：完成支付
	t.Log("=== 方法4：完成支付 ===")
	logger.Info(ctx, "Payment completed successfully", map[string]interface{}{
		"transaction_id": "txn_123456789",
		"order_id":       "11698228134255",
		"status":         "success",
	})

	traceInfo4 := GetTraceContext(ctx)
	t.Logf("方法4的 Trace ID: %s", traceInfo4.TraceID)
	t.Logf("方法4的 Span ID: %s", traceInfo4.SpanID)

	// 检查所有方法的trace ID是否一致
	allTraceIDs := []string{traceInfo1.TraceID, traceInfo2.TraceID, traceInfo3.TraceID, traceInfo4.TraceID}
	allSpanIDs := []string{traceInfo1.SpanID, traceInfo2.SpanID, traceInfo3.SpanID, traceInfo4.SpanID}

	// 验证所有trace ID都相同
	for i := 1; i < len(allTraceIDs); i++ {
		if allTraceIDs[0] != allTraceIDs[i] {
			t.Errorf("Trace ID should be consistent across methods, got %s and %s", allTraceIDs[0], allTraceIDs[i])
		}
	}

	// 验证所有span ID都相同
	for i := 1; i < len(allSpanIDs); i++ {
		if allSpanIDs[0] != allSpanIDs[i] {
			t.Errorf("Span ID should be consistent across methods, got %s and %s", allSpanIDs[0], allSpanIDs[i])
		}
	}

	t.Logf("=== 验证结果 ===")
	t.Logf("所有方法使用相同的 Trace ID: %s", allTraceIDs[0])
	t.Logf("所有方法使用相同的 Span ID: %s", allSpanIDs[0])
	t.Log("✅ 测试通过：不同方法间的trace ID保持一致")
}

func TestNilContextHandling(t *testing.T) {
	// 初始化配置
	config := NewConfig("test-service", "1.0.0").WithLogging("info", "json", "stdout")
	logger, err := NewLogger(config)
	if err != nil {
		t.Fatalf("创建日志器失败: %v", err)
	}

	t.Log("=== 测试 nil context 处理 ===")

	// 测试1：nil context + nil fields
	t.Log("1. 测试 nil context + nil fields")
	logger.Info(nil, "测试 nil context 和 nil fields", nil)
	logger.Error(nil, "测试 nil context 和 nil fields", nil)
	logger.Warn(nil, "测试 nil context 和 nil fields", nil)
	logger.Debug(nil, "测试 nil context 和 nil fields", nil)

	// 测试2：nil context + valid fields
	t.Log("2. 测试 nil context + valid fields")
	logger.Info(nil, "测试 nil context 和 valid fields", map[string]interface{}{
		"key1": "value1",
		"key2": 123,
	})

	// 测试3：valid context + nil fields
	t.Log("3. 测试 valid context + nil fields")
	ctx := context.Background()
	ctx = WithRequestID(ctx, "req_test_001")
	ctx = WithUserID(ctx, "user_test_001")
	logger.Info(ctx, "测试 valid context 和 nil fields", nil)

	// 测试4：nil context + empty fields
	t.Log("4. 测试 nil context + empty fields")
	logger.Info(nil, "测试 nil context 和 empty fields", map[string]interface{}{})

	t.Log("✅ 测试通过：所有 nil 处理测试都正常完成")
}

func TestNilFieldsHandling(t *testing.T) {
	// 初始化配置
	config := NewConfig("test-service", "1.0.0").WithLogging("info", "json", "stdout")
	logger, err := NewLogger(config)
	if err != nil {
		t.Fatalf("创建日志器失败: %v", err)
	}

	t.Log("=== 测试 nil fields 处理 ===")

	ctx := context.Background()
	ctx = WithRequestID(ctx, "req_test_002")

	// 测试1：nil fields
	t.Log("1. 测试 nil fields")
	logger.Info(ctx, "测试消息 1", nil)
	logger.Error(ctx, "测试消息 2", nil)
	logger.Warn(ctx, "测试消息 3", nil)
	logger.Debug(ctx, "测试消息 4", nil)

	// 测试2：empty fields
	t.Log("2. 测试 empty fields")
	logger.Info(ctx, "测试消息 5", map[string]interface{}{})

	// 测试3：InfoNoCtx with nil fields
	t.Log("3. 测试 InfoNoCtx with nil fields")
	logger.InfoNoCtx("测试消息 6", nil)
	logger.ErrorNoCtx("测试消息 7", nil)
	logger.WarnNoCtx("测试消息 8", nil)
	logger.DebugNoCtx("测试消息 9", nil)

	t.Log("✅ 测试通过：所有 nil fields 处理测试都正常完成")
}
