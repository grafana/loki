// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package azidentity

import (
	"fmt"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/internal/diag"
	"github.com/Azure/azure-sdk-for-go/sdk/internal/log"
)

// EventAuthentication entries contain information about authentication.
// This includes information like the names of environment variables
// used when obtaining credentials and the type of credential used.
const EventAuthentication log.Event = "Authentication"

func logGetTokenSuccess(cred azcore.TokenCredential, opts policy.TokenRequestOptions) {
	if !log.Should(EventAuthentication) {
		return
	}
	msg := fmt.Sprintf("Azure Identity => GetToken() result for %T: SUCCESS\n", cred)
	msg += fmt.Sprintf("\tCredential Scopes: [%s]", strings.Join(opts.Scopes, ", "))
	log.Write(EventAuthentication, msg)
}

func logCredentialError(credName string, err error) {
	log.Writef(EventAuthentication, "Azure Identity => ERROR in %s: %s", credName, err.Error())
}

func addGetTokenFailureLogs(credName string, err error, includeStack bool) {
	if !log.Should(EventAuthentication) {
		return
	}
	stack := ""
	if includeStack {
		// skip the stack trace frames and ourself
		stack = "\n" + diag.StackTrace(3, 32)
	}
	log.Writef(EventAuthentication, "Azure Identity => ERROR in GetToken() call for %s: %s%s", credName, err.Error(), stack)
}
