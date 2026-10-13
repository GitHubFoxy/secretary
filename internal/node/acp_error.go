package node

import (
	"encoding/json"
	"strings"

	"github.com/beruseruko/secretary/internal/acp"
	"github.com/beruseruko/secretary/internal/core"
)

type SteeringError struct {
	Failure core.WorkerFailure
	Err     error
}

func (e *SteeringError) Error() string {
	if e.Failure.Message != "" {
		return SanitizeFailureMessage(e.Failure.Message)
	}
	if e.Err == nil {
		return "worker steering failed"
	}
	return SanitizeFailureMessage(e.Err.Error())
}

func (e *SteeringError) Unwrap() error {
	return e.Err
}

func SanitizeProviderCode(code string) string {
	return core.SanitizeFailureProviderCode(code)
}

func classifyACPError(rpcErr *acp.RPCError, collectedText string) core.WorkerFailure {
	technical := ""
	if rpcErr != nil {
		technical = SanitizeFailureMessage(rpcErr.Error())
	}
	failure := core.WorkerFailure{
		Category:  core.FailureCategoryInternalError,
		Technical: technical,
		Message:   technical,
	}
	if rpcErr == nil {
		return failure
	}

	if len(rpcErr.Data) > 0 && string(rpcErr.Data) != "null" && len(rpcErr.Data) <= 64*1024 {
		var dataObj struct {
			Message             string `json:"message"`
			CodexErrorInfo      string `json:"codexErrorInfo"`
			CodexErrorInfoSnake string `json:"codex_error_info"`
			Code                string `json:"code"`
			Error               string `json:"error"`
			Type                string `json:"type"`
		}
		if err := json.Unmarshal(rpcErr.Data, &dataObj); err == nil {
			rawProviderCode := dataObj.CodexErrorInfo
			if rawProviderCode == "" {
				rawProviderCode = dataObj.CodexErrorInfoSnake
			}
			if rawProviderCode == "" {
				rawProviderCode = dataObj.Code
			}
			if rawProviderCode == "" {
				rawProviderCode = dataObj.Type
			}
			providerMsg := dataObj.Message
			if providerMsg == "" {
				providerMsg = dataObj.Error
			}
			providerMessage := SanitizeFailureMessage(providerMsg)

			providerCode := SanitizeProviderCode(rawProviderCode)

			switch {
			case strings.EqualFold(providerCode, "usage_limit_exceeded") || strings.EqualFold(rawProviderCode, "usageLimitExceeded"):
				failure.Category = core.FailureCategoryUsageLimit
				failure.ProviderCode = "usage_limit_exceeded"
				failure.Message = providerMessage
				if failure.Message == "" {
					failure.Message = "Лимит использования исчерпан"
				}
				return failure

			case isAuthError(providerCode):
				failure.Category = core.FailureCategoryAuthRequired
				failure.ProviderCode = providerCode
				failure.Message = providerMessage
				if failure.Message == "" {
					failure.Message = "Требуется авторизация"
				}
				return failure

			case isBillingCreditsError(providerCode):
				failure.Category = core.FailureCategoryBillingCredits
				failure.ProviderCode = providerCode
				failure.Message = providerMessage
				if failure.Message == "" {
					failure.Message = "Недостаточно средств на балансе"
				}
				return failure

			case isRateLimitError(providerCode):
				failure.Category = core.FailureCategoryRateLimit
				failure.ProviderCode = providerCode
				failure.Message = providerMessage
				if failure.Message == "" {
					failure.Message = "Превышен лимит запросов"
				}
				return failure

			default:
				failure.ProviderCode = ""
				failure.Message = providerMessage
				if failure.Message != "" {
					return failure
				}
			}
		}
	}

	failure.Message = SanitizeFailureMessage(technical)
	return failure
}

func isAuthError(code string) bool {
	codeLower := strings.ToLower(code)
	switch codeLower {
	case "auth_required", "invalid_api_key", "token_expired", "unauthorized", "authentication_required":
		return true
	}
	return false
}

func isBillingCreditsError(code string) bool {
	codeLower := strings.ToLower(code)
	return codeLower == "insufficient_quota" || codeLower == "billing_credits"
}

func isRateLimitError(code string) bool {
	codeLower := strings.ToLower(code)
	switch codeLower {
	case "rate_limit", "rate_limit_exceeded", "too_many_requests":
		return true
	}
	return false
}
