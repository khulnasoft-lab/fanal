// SPDX-License-Identifier: Apache-2.0
/*
Copyright (C) 2023 The KhulnaSoft Authors.
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at
    http://www.apache.org/licenses/LICENSE-2.0
Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"path/filepath"

	"github.com/kyma-project/test-infra/development/tools/pkg/file"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

type options struct {
	configPath    string
	jobConfigPath string
	pluginConfig  string
	kubeconfig    string
}

const (
	namespace = "default"
)

func gatherOptions() options {
	o := options{}
	flag.StringVar(&o.configPath, "config-path", "", "Path to config file.")
	flag.StringVar(&o.jobConfigPath, "jobs-config-path", "", "Path to prow job configs.")
	flag.StringVar(&o.pluginConfig, "plugins-config-path", "", "Path to plugins config file.")
	flag.StringVar(&o.kubeconfig, "kubeconfig", "", "Path to kubeconfig file. By default authentication with Service Account token is used.")
	flag.Parse()
	return o
}

func main() {
	o := gatherOptions()

	var config *rest.Config

	if o.kubeconfig != "" {
		var err error
		config, err = clientcmd.BuildConfigFromFlags("", o.kubeconfig)
		exitOnError(err, "while loading kubeconfig")
	} else {
		var err error
		config, err = rest.InClusterConfig()
		exitOnError(err, "while loading service account token")
	}

	client, err := kubernetes.NewForConfig(config)
	exitOnError(err, "while creating kube client")

	configMapClient := client.CoreV1().ConfigMaps(namespace)

	if o.pluginConfig != "" {
		logrus.Info("Updating plugins")
		err = replaceConfigMapFromFile("plugins", o.pluginConfig, configMapClient)
		exitOnError(err, "while updating plugins")
		logrus.Info("Updating plugins finished")
	}

	if o.configPath != "" {
		logrus.Info("Updating config")
		err = replaceConfigMapFromFile("config", o.configPath, configMapClient)
		exitOnError(err, "while updating config")
		logrus.Info("Updating config finished")
	}

	if o.jobConfigPath != "" {
		logrus.Info("Updating jobs")
		err = replaceConfigMapFromDirectory("job-config", o.jobConfigPath, configMapClient)
		exitOnError(err, "while updating jobs")
		logrus.Info("Updating jobs finished")
	}
}

func exitOnError(err error, context string) {
	if err != nil {
		logrus.Fatal(errors.Wrap(err, context))
	}
}

type configMapSetter interface {
	Update(*v1.ConfigMap) (*v1.ConfigMap, error)
}

func replaceConfigMapFromFile(name, path string, client configMapSetter) error {
	config, err := configMapFromFile(name, path)
	if err != nil {
		return err
	}

	_, err = client.Update(config)
	return err
}

func replaceConfigMapFromDirectory(name, path string, client configMapSetter) error {
	config, err := configMapFromYamlsInDirectory(name, path)
	if err != nil {
		return err
	}

	_, err = client.Update(config)
	return err
}

func configMapFromFile(name, path string) (*v1.ConfigMap, error) {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}

	filename := fmt.Sprintf("%s.yaml", name)

	result := v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Data: map[string]string{
			filename: string(data),
		},
	}

	return &result, nil
}

func configMapFromYamlsInDirectory(name, rootPath string) (*v1.ConfigMap, error) {
	paths, err := file.FindAllRecursively(rootPath, ".yaml")
	if err != nil {
		return nil, err
	}

	configData := make(map[string]string, len(paths))
	for _, path := range paths {
		data, _ := ioutil.ReadFile(path)
		filename := filepath.Base(path)

		configData[filename] = string(data)
	}

	result := v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Data: configData,
	}

	return &result, nil
}
