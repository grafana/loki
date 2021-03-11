/* -*- Mode: C; tab-width: 4; indent-tabs-mode: nil; c-basic-offset: 4 -*- */

/*  Fluent Bit Go!
 *  ==============
 *  Copyright (C) 2015-2017 Treasure Data Inc.
 *
 *  Licensed under the Apache License, Version 2.0 (the "License");
 *  you may not use this file except in compliance with the License.
 *  You may obtain a copy of the License at
 *
 *      http://www.apache.org/licenses/LICENSE-2.0
 *
 *  Unless required by applicable law or agreed to in writing, software
 *  distributed under the License is distributed on an "AS IS" BASIS,
 *  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 *  See the License for the specific language governing permissions and
 *  limitations under the License.
 */

#ifndef FLBGO_PLUGIN_H
#define FLBGO_PLUGIN_H

/* Return values */
#define FLB_ERROR   0
#define FLB_OK      1
#define FLB_RETRY   2

/* Proxy definition */
#define FLB_PROXY_OUTPUT_PLUGIN    2
#define FLB_PROXY_GOLANG          11

/* This structure is used for registration.
 * It matches the one in flb_plugin_proxy.h in fluent-bit source code.
 */
struct flb_plugin_proxy_def {
    int type;
    int proxy;
    int flags;
    char *name;
    char *description;
};

#endif
