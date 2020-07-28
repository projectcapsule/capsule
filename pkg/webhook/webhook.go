/*
Copyright 2020 Clastix Labs.
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

package webhook

import (
	"io/ioutil"

	"sigs.k8s.io/controller-runtime/pkg/manager"
)

// AddToWebhookServer is a list of functions to create webhooks and add them to a manager.
var AddToWebhookServer []func(manager2 manager.Manager) error

// AddToServer adds all Controllers to the Manager
func AddToServer(mgr manager.Manager) error {
	// skipping webhook setup if certificate is missing
	dat, _ := ioutil.ReadFile("/tmp/k8s-webhook-server/serving-certs/tls.crt")
	if len(dat) == 0 {
		return nil
	}
	for _, f := range AddToWebhookServer {
		if err := f(mgr); err != nil {
			return err
		}
	}
	return nil
}
