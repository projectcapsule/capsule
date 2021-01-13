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

package cert

import "time"

type CertificateOptions interface {
	DNSNames() []string
	ExpirationDate() time.Time
}

type certOpts struct {
	dnsNames       []string
	expirationDate time.Time
}

func (c certOpts) DNSNames() []string {
	return c.dnsNames
}

func (c certOpts) ExpirationDate() time.Time {
	return c.expirationDate
}

func NewCertOpts(expirationDate time.Time, dnsNames ...string) CertificateOptions {
	return &certOpts{dnsNames: dnsNames, expirationDate: expirationDate}
}
