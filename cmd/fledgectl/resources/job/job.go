// Copyright (c) 2021 Cisco Systems, Inc. and its affiliates
// All rights reserved
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package job

import (
	"encoding/json"
	"fmt"
	"io/ioutil"

	"wwwin-github.cisco.com/eti/fledge/cmd/fledgectl/resources"
	"wwwin-github.cisco.com/eti/fledge/pkg/openapi"
	"wwwin-github.cisco.com/eti/fledge/pkg/restapi"
)

type Params struct {
	resources.CommonParams

	JobFile   string
	DatasetId string
}

func Create(params Params) error {
	data, err := ioutil.ReadFile(params.JobFile)
	if err != nil {
		fmt.Printf("Failed to read file %s: %v\n", params.JobFile, err)
		return nil
	}

	// encode the data
	jobSpec := openapi.JobSpec{}
	err = json.Unmarshal(data, &jobSpec)
	if err != nil {
		fmt.Printf("Failed to parse %s\n", params.JobFile)
		return nil
	}

	// construct URL
	uriMap := map[string]string{
		"user": params.User,
	}
	url := restapi.CreateURL(params.Host, params.Port, restapi.CreateJobEndpoint, uriMap)

	// send post request
	code, resp, err := restapi.HTTPPost(url, jobSpec, "application/json")
	if err != nil || restapi.CheckStatusCode(code) != nil {
		fmt.Printf("Failed to create a job - code: %d, error: %v\n", code, err)
		return nil
	}

	jobStatus := openapi.JobStatus{}
	err = json.Unmarshal(resp, &jobStatus)
	if err != nil {
		fmt.Printf("WARNING: Failed to parse resp message: %v", err)
		return nil
	}

	fmt.Printf("New job created successfully\n")
	fmt.Printf("\tID: %s\n", jobStatus.Id)
	fmt.Printf("\tstate: %s\n", jobStatus.State)

	return nil
}