package oa

// Known Zalo OA error codes. The access-token-invalid family is returned
// with inconsistent sign + magnitude (216, -216, 401, -401) for the same
// cause; all four are treated identically.
const (
	// Access token invalid/expired → ForceRefresh + one retry.
	codeAccessTokenInvalid216Neg = -216
	codeAccessTokenInvalid216Pos = 216
	codeAccessTokenInvalid401Neg = -401
	codeAccessTokenInvalid401Pos = 401

	// Refresh token dead → operator must re-consent.
	codeInvalidGrant = -118

	// Payload shape rejected (e.g. send endpoint requires template/media
	// shape for images instead of plain attachment_id).
	codeParamsInvalid = -201

	// Upload body exceeds the endpoint cap (image 1MB, file 5MB, gif 5MB).
	codeFileSizeExceeded = -210

	// OAuth: redirect_uri does not match the one registered on Zalo console.
	codeInvalidRedirectURI = -14003
)

// isAccessTokenInvalid reports whether code is in the access-token
// invalid/expired family.
func isAccessTokenInvalid(code int) bool {
	switch code {
	case codeAccessTokenInvalid216Neg, codeAccessTokenInvalid216Pos,
		codeAccessTokenInvalid401Neg, codeAccessTokenInvalid401Pos:
		return true
	}
	return false
}
