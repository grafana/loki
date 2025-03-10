/*
 * MinIO Go Library for Amazon S3 Compatible Cloud Storage
 * Copyright 2015-2024 MinIO, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package minio

type awsS3Endpoint struct {
	endpoint          string
	dualstackEndpoint string
}

// awsS3EndpointMap Amazon S3 endpoint map.
var awsS3EndpointMap = map[string]awsS3Endpoint{
	"us-east-1": {
		"s3.us-east-1.amazonaws.com",
		"s3.dualstack.us-east-1.amazonaws.com",
	},
	"us-east-2": {
		"s3.us-east-2.amazonaws.com",
		"s3.dualstack.us-east-2.amazonaws.com",
	},
	"us-iso-east-1": {
		"s3.us-iso-east-1.c2s.ic.gov",
		"s3.dualstack.us-iso-east-1.c2s.ic.gov",
	},
	"us-isob-east-1": {
		"s3.us-isob-east-1.sc2s.sgov.gov",
		"s3.dualstack.us-isob-east-1.sc2s.sgov.gov",
	},
	"us-iso-west-1": {
		"s3.us-iso-west-1.c2s.ic.gov",
		"s3.dualstack.us-iso-west-1.c2s.ic.gov",
	},
	"us-west-2": {
		"s3.us-west-2.amazonaws.com",
		"s3.dualstack.us-west-2.amazonaws.com",
	},
	"us-west-1": {
		"s3.us-west-1.amazonaws.com",
		"s3.dualstack.us-west-1.amazonaws.com",
	},
	"ca-central-1": {
		"s3.ca-central-1.amazonaws.com",
		"s3.dualstack.ca-central-1.amazonaws.com",
	},
	"ca-west-1": {
		"s3.ca-west-1.amazonaws.com",
		"s3.dualstack.ca-west-1.amazonaws.com",
	},
	"eu-west-1": {
		"s3.eu-west-1.amazonaws.com",
		"s3.dualstack.eu-west-1.amazonaws.com",
	},
	"eu-west-2": {
		"s3.eu-west-2.amazonaws.com",
		"s3.dualstack.eu-west-2.amazonaws.com",
	},
	"eu-west-3": {
		"s3.eu-west-3.amazonaws.com",
		"s3.dualstack.eu-west-3.amazonaws.com",
	},
	"eu-central-1": {
		"s3.eu-central-1.amazonaws.com",
		"s3.dualstack.eu-central-1.amazonaws.com",
	},
	"eu-central-2": {
		"s3.eu-central-2.amazonaws.com",
		"s3.dualstack.eu-central-2.amazonaws.com",
	},
	"eu-north-1": {
		"s3.eu-north-1.amazonaws.com",
		"s3.dualstack.eu-north-1.amazonaws.com",
	},
	"eu-south-1": {
		"s3.eu-south-1.amazonaws.com",
		"s3.dualstack.eu-south-1.amazonaws.com",
	},
	"eu-south-2": {
		"s3.eu-south-2.amazonaws.com",
		"s3.dualstack.eu-south-2.amazonaws.com",
	},
	"ap-east-1": {
		"s3.ap-east-1.amazonaws.com",
		"s3.dualstack.ap-east-1.amazonaws.com",
	},
	"ap-south-1": {
		"s3.ap-south-1.amazonaws.com",
		"s3.dualstack.ap-south-1.amazonaws.com",
	},
	"ap-south-2": {
		"s3.ap-south-2.amazonaws.com",
		"s3.dualstack.ap-south-2.amazonaws.com",
	},
	"ap-southeast-1": {
		"s3.ap-southeast-1.amazonaws.com",
		"s3.dualstack.ap-southeast-1.amazonaws.com",
	},
	"ap-southeast-2": {
		"s3.ap-southeast-2.amazonaws.com",
		"s3.dualstack.ap-southeast-2.amazonaws.com",
	},
	"ap-southeast-3": {
		"s3.ap-southeast-3.amazonaws.com",
		"s3.dualstack.ap-southeast-3.amazonaws.com",
	},
	"ap-southeast-4": {
		"s3.ap-southeast-4.amazonaws.com",
		"s3.dualstack.ap-southeast-4.amazonaws.com",
	},
	"ap-northeast-1": {
		"s3.ap-northeast-1.amazonaws.com",
		"s3.dualstack.ap-northeast-1.amazonaws.com",
	},
	"ap-northeast-2": {
		"s3.ap-northeast-2.amazonaws.com",
		"s3.dualstack.ap-northeast-2.amazonaws.com",
	},
	"ap-northeast-3": {
		"s3.ap-northeast-3.amazonaws.com",
		"s3.dualstack.ap-northeast-3.amazonaws.com",
	},
	"af-south-1": {
		"s3.af-south-1.amazonaws.com",
		"s3.dualstack.af-south-1.amazonaws.com",
	},
	"me-central-1": {
		"s3.me-central-1.amazonaws.com",
		"s3.dualstack.me-central-1.amazonaws.com",
	},
	"me-south-1": {
		"s3.me-south-1.amazonaws.com",
		"s3.dualstack.me-south-1.amazonaws.com",
	},
	"sa-east-1": {
		"s3.sa-east-1.amazonaws.com",
		"s3.dualstack.sa-east-1.amazonaws.com",
	},
	"us-gov-west-1": {
		"s3.us-gov-west-1.amazonaws.com",
		"s3.dualstack.us-gov-west-1.amazonaws.com",
	},
	"us-gov-east-1": {
		"s3.us-gov-east-1.amazonaws.com",
		"s3.dualstack.us-gov-east-1.amazonaws.com",
	},
	"cn-north-1": {
		"s3.cn-north-1.amazonaws.com.cn",
		"s3.dualstack.cn-north-1.amazonaws.com.cn",
	},
	"cn-northwest-1": {
		"s3.cn-northwest-1.amazonaws.com.cn",
		"s3.dualstack.cn-northwest-1.amazonaws.com.cn",
	},
	"il-central-1": {
		"s3.il-central-1.amazonaws.com",
		"s3.dualstack.il-central-1.amazonaws.com",
	},
}

// getS3Endpoint get Amazon S3 endpoint based on the bucket location.
func getS3Endpoint(bucketLocation string, useDualstack bool) (endpoint string) {
	s3Endpoint, ok := awsS3EndpointMap[bucketLocation]
	if !ok {
		// Default to 's3.us-east-1.amazonaws.com' endpoint.
		if useDualstack {
			return "s3.dualstack.us-east-1.amazonaws.com"
		}
		return "s3.us-east-1.amazonaws.com"
	}
	if useDualstack {
		return s3Endpoint.dualstackEndpoint
	}
	return s3Endpoint.endpoint
}
