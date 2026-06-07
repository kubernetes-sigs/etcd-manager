/*
Copyright 2026 The Kubernetes Authors.

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

package sysmount

// Mounter exposes mount operations backed by mount(2), avoiding a dependency on
// a mount binary in distroless-based images.
type Mounter struct{}

// New returns a syscall-backed mounter.
func New() *Mounter {
	return &Mounter{}
}

func (m *Mounter) Mount(source string, target string, fstype string, options []string) error {
	return Mount(source, target, fstype, options)
}

func (m *Mounter) Unmount(target string) error {
	return Unmount(target)
}
