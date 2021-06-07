// Package auth provides an abstraction over claims-based security for Azure Event Hub and Service Bus.
package auth

//	MIT License
//
//	Copyright (c) Microsoft Corporation. All rights reserved.
//
//	Permission is hereby granted, free of charge, to any person obtaining a copy
//	of this software and associated documentation files (the "Software"), to deal
//	in the Software without restriction, including without limitation the rights
//	to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
//	copies of the Software, and to permit persons to whom the Software is
//	furnished to do so, subject to the following conditions:
//
//	The above copyright notice and this permission notice shall be included in all
//	copies or substantial portions of the Software.
//
//	THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
//	IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
//	FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
//	AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
//	LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
//	OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
//	SOFTWARE

const (
	// CBSTokenTypeJWT is the type of token to be used for JWTs. For example Azure Active Directory tokens.
	CBSTokenTypeJWT TokenType = "jwt"
	// CBSTokenTypeSAS is the type of token to be used for SAS tokens.
	CBSTokenTypeSAS TokenType = "servicebus.windows.net:sastoken"
)

type (
	// TokenType represents types of tokens known for claims-based auth
	TokenType string

	// Token contains all of the information to negotiate authentication
	Token struct {
		// TokenType is the type of CBS token
		TokenType TokenType
		Token     string
		Expiry    string
	}

	// TokenProvider abstracts the fetching of authentication tokens
	TokenProvider interface {
		GetToken(uri string) (*Token, error)
	}
)

// NewToken constructs a new auth token
func NewToken(tokenType TokenType, token, expiry string) *Token {
	return &Token{
		TokenType: tokenType,
		Token:     token,
		Expiry:    expiry,
	}
}
