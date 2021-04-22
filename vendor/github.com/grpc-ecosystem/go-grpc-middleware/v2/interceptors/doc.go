// Copyright 2017 Michal Witkowski. All Rights Reserved.
// See LICENSE for licensing terms.

//
/*
interceptor is an internal package used by higher level middlewares. It allows injecting custom code in various
places of the gRPC lifecycle.

This particular package is intended for use by other middleware, metric, logging or otherwise.
This allows code to be shared between different implementations.
*/
package interceptors
