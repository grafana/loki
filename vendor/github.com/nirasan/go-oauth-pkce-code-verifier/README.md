# go-oauth-pkce-code-verifier
[OAuth PKCE](https://tools.ietf.org/html/rfc7636) code_verifier and code_challenge generator implimented golang.

# How to use

```go
// Create code_verifier
v := CreateCodeVerifier()
code_verifier := v.String()

// Create code_challenge with plain method
code_challenge := v.CodeChallengeS256()
code_challenge_method := "plain"

// Create code_challenge with S256 method
code_challenge := v.CodeChallengeS256()
code_challenge_method := "S256"
```