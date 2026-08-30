package domain

type Envelope struct {
	Code      int         `json:"code"`
	Message   string      `json:"message"`
	Data      interface{} `json:"data"`
	RequestID string      `json:"request_id"`
	Timestamp string      `json:"timestamp"`
}

type ErrorDef struct {
	HTTPStatus int
	Code       int
	Message    string
}

var (
	ErrInvalidParameters     = ErrorDef{HTTPStatus: 400, Code: 10001, Message: "invalid parameters"}
	ErrResourceNotFound      = ErrorDef{HTTPStatus: 404, Code: 10002, Message: "resource not found"}
	ErrPermissionDenied      = ErrorDef{HTTPStatus: 403, Code: 10003, Message: "permission denied"}
	ErrSignatureVerification = ErrorDef{HTTPStatus: 401, Code: 10004, Message: "signature verification failed"}
	ErrInsufficientBalance   = ErrorDef{HTTPStatus: 402, Code: 20001, Message: "insufficient balance"}
	ErrPaymentAlreadyExists  = ErrorDef{HTTPStatus: 409, Code: 20002, Message: "payment already exists"}
	ErrBlockchainNetwork     = ErrorDef{HTTPStatus: 503, Code: 20003, Message: "blockchain network error"}
	ErrGasSubsidyFailed      = ErrorDef{HTTPStatus: 500, Code: 20004, Message: "gas subsidy failed"}
	ErrInternalServer        = ErrorDef{HTTPStatus: 500, Code: 30001, Message: "internal server error"}
	ErrServiceUnavailable    = ErrorDef{HTTPStatus: 503, Code: 30002, Message: "service unavailable"}
	ErrDatabaseConnection    = ErrorDef{HTTPStatus: 503, Code: 30003, Message: "database connection error"}
	ErrRateLimitExceeded     = ErrorDef{HTTPStatus: 429, Code: 30004, Message: "rate limit exceeded"}
)
