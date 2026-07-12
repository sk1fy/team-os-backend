// Package auth contains the service-neutral JWT contract used by TeamOS.
package auth

import "github.com/golang-jwt/jwt/v5"

// Claims are deliberately self-contained so domain services can authorize a
// request without a synchronous lookup in the company service.
type Claims struct {
	CompanyID     string   `json:"cid"`
	Role          string   `json:"role"`
	PositionIDs   []string `json:"pos,omitempty"`
	DepartmentIDs []string `json:"dep,omitempty"`
	jwt.RegisteredClaims
}
