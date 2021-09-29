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

package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	backoff "github.com/cenkalti/backoff/v4"
	"go.uber.org/zap"
	"google.golang.org/grpc"

	pbNotify "wwwin-github.cisco.com/eti/fledge/pkg/proto/notification"
	"wwwin-github.cisco.com/eti/fledge/pkg/restapi"
	"wwwin-github.cisco.com/eti/fledge/pkg/util"
)

const (
	workDir       = "/fledge/work"
	pythonBin     = "python3"
	taskPyFile    = "main.py"
	logFilePrefix = "task"
	logFileExt    = "log"
)

type taskHandler struct {
	apiserverEp string
	notifierEp  string
	name        string
	agentId     string

	stream pbNotify.EventRoute_GetEventClient

	// role of the task given to fledgelet
	role string
}

func newTaskHandler(apiserverEp string, notifierEp string, name string, agentId string) *taskHandler {
	return &taskHandler{
		apiserverEp: apiserverEp,
		notifierEp:  notifierEp,
		name:        name,
		agentId:     agentId,
	}
}

// start connects to the notifier via grpc and handles notifications from the notifier
func (t *taskHandler) start() {
	go t.doStart()
}

func (t *taskHandler) doStart() {
	pauseTime := 10 * time.Second

	for {
		expBackoff := backoff.NewExponentialBackOff()
		expBackoff.MaxElapsedTime = 5 * time.Minute // max wait time: 5 minutes
		err := backoff.Retry(t.connect, expBackoff)
		if err != nil {
			zap.S().Fatalf("Cannot connect with notifier: %v", err)
		}

		t.do()

		// if connection is broken right after connection is made, this can cause
		// too many connection/disconnection events. To migitage that, add some static
		// pause time.
		time.Sleep(pauseTime)
	}
}

func (t *taskHandler) connect() error {
	// dial server
	conn, err := grpc.Dial(t.notifierEp, grpc.WithInsecure())
	if err != nil {
		zap.S().Debugf("Cannot connect with notifier: %v", err)
		return err
	}

	client := pbNotify.NewEventRouteClient(conn)
	in := &pbNotify.AgentInfo{
		Id:       t.agentId,
		Hostname: t.name,
	}

	// setup notification stream
	stream, err := client.GetEvent(context.Background(), in)
	if err != nil {
		zap.S().Debugf("Open stream error: %v", err)
		return err
	}

	t.stream = stream
	zap.S().Infof("Connected with notifier at %s", t.notifierEp)

	return nil
}

func (t *taskHandler) do() {
	for {
		resp, err := t.stream.Recv()
		if err != nil {
			zap.S().Errorf("Failed to receive notification: %v", err)
			break
		}

		t.dealWith(resp)
	}

	zap.S().Info("Disconnected from notifier")
}

//newNotification acts as a handler and calls respective functions based on the response type to act on the received notifications.
func (t *taskHandler) dealWith(in *pbNotify.Event) {
	switch in.GetType() {
	case pbNotify.EventType_START_JOB:
		t.startJob(in.JobId)

	case pbNotify.EventType_STOP_JOB:
		t.stopJob(in.JobId)

	case pbNotify.EventType_UPDATE_JOB:
		t.updateJob(in.JobId)

	case pbNotify.EventType_UNKNOWN_EVENT_TYPE:
		fallthrough
	default:
		zap.S().Errorf("Invalid message type: %s", in.GetType())
	}
}

// startJob starts the application on the agent
func (t *taskHandler) startJob(jobId string) {
	zap.S().Infof("Received start job request on job %s", jobId)

	filePaths, err := t.getTask(jobId)
	if err != nil {
		zap.S().Warnf("Failed to download payload: %v", err)
		return
	}

	err = t.prepareTask(filePaths)
	if err != nil {
		zap.S().Warnf("Failed to prepare task")
		return
	}

	go t.runTask(jobId)

	// TODO: implement updateTaskStatus method
}

func (t *taskHandler) getTask(jobId string) ([]string, error) {
	// construct URL
	uriMap := map[string]string{
		"jobId":   jobId,
		"agentId": t.agentId,
	}
	url := restapi.CreateURL(t.apiserverEp, restapi.GetTaskEndpoint, uriMap)

	code, taskMap, err := restapi.HTTPGetMultipart(url)
	if err != nil || restapi.CheckStatusCode(code) != nil {
		errMsg := fmt.Sprintf("Failed to fetch task - code: %d, error: %v", code, err)
		zap.S().Warnf(errMsg)
		return nil, fmt.Errorf(errMsg)
	}

	filePaths := make([]string, 0)
	for fileName, data := range taskMap {
		filePath := filepath.Join("/tmp", fileName)
		err = ioutil.WriteFile(filePath, data, util.FilePerm0755)
		if err != nil {
			zap.S().Warnf("Failed to save %s: %v\n", fileName, err)
			return nil, err
		}

		filePaths = append(filePaths, filePath)

		zap.S().Infof("Downloaded %s successfully\n", fileName)
	}

	return filePaths, nil
}

func (t *taskHandler) prepareTask(filePaths []string) error {
	err := os.MkdirAll(workDir, util.FilePerm0755)
	if err != nil {
		return err
	}

	var fileDataList []util.FileData
	var file *os.File
	configFilePath := ""
	configFound := false
	codeFound := false
	for _, filePath := range filePaths {
		if strings.Contains(filePath, util.TaskConfigFile) {
			configFound = true

			configFilePath = filePath
		} else if strings.Contains(filePath, util.TaskCodeFile) {
			codeFound = true

			file, err = os.Open(filePath)
			if err != nil {
				return fmt.Errorf("failed to open %s: %v", filePath, err)
			}

			fileDataList, err = util.UnzipFile(file)
			if err != nil {
				return err
			}
		}
	}

	if !configFound || !codeFound {
		return fmt.Errorf("either %s or %s not found", util.TaskConfigFile, util.TaskCodeFile)
	}

	// copy config file to work directory
	input, err := ioutil.ReadFile(configFilePath)
	if err != nil {
		return fmt.Errorf("failed to open config file %s: %v", configFilePath, err)
	}

	dstFilePath := filepath.Join(workDir, util.TaskConfigFile)
	err = ioutil.WriteFile(dstFilePath, input, util.FilePerm0644)
	if err != nil {
		return fmt.Errorf("failed to copy config file: %v", err)
	}

	type tmpStruct struct {
		Role string `json:"role"`
	}

	tmp := tmpStruct{}

	err = json.Unmarshal(input, &tmp)
	if err != nil {
		return fmt.Errorf("failed to parse role")
	}
	t.role = tmp.Role

	// copy code files to work directory
	for _, fileData := range fileDataList {
		dirPath := filepath.Join(workDir, filepath.Dir(fileData.FullName))
		err := os.MkdirAll(dirPath, util.FilePerm0755)
		if err != nil {
			return fmt.Errorf("failed to create directory: %v", err)
		}

		filePath := filepath.Join(dirPath, fileData.BaseName)
		err = ioutil.WriteFile(filePath, []byte(fileData.Data), util.FilePerm0644)
		if err != nil {
			return fmt.Errorf("failed to unzip file %s: %v", filePath, err)
		}
	}

	return nil
}

func (t *taskHandler) stopJob(jobId string) {
	zap.S().Infof("not yet implemented; received stop job request on job %s", jobId)
}

func (t *taskHandler) updateJob(jobId string) (string, error) {
	zap.S().Infof("not yet implemented; received update job request on job %s", jobId)
	return "", nil
}

func (t *taskHandler) runTask(jobId string) {
	taskFilePath := filepath.Join(workDir, t.role, taskPyFile)
	configFilePath := filepath.Join(workDir, util.TaskConfigFile)

	// TODO: run the task in different user group with less privilege
	cmd := exec.Command(pythonBin, taskFilePath, configFilePath)
	zap.S().Debugf("Running task with command: %v", cmd)

	logFileName := fmt.Sprintf("%s-%s.%s", logFilePrefix, jobId, logFileExt)
	logFilePath := filepath.Join(util.LogDirPath, logFileName)
	file, err := os.Create(logFilePath)
	if err != nil {
		zap.S().Errorf("Failed to create a log file: %v", err)
		return
	}
	defer file.Close()

	cmd.Stdout = file
	cmd.Stderr = file

	err = cmd.Start()
	if err != nil {
		zap.S().Errorf("Failed to start task: %v", err)
		return
	}

	zap.S().Infof("Started task for job %s successfully", jobId)
}
