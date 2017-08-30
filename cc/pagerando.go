// Copyright 2017 Google Inc. All rights reserved.
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

package cc

import (
	"fmt"
	"io"

	"github.com/google/blueprint/proptools"

	"android/soong/android"
)

const (
	pagerandoCFlags = "-fpagerando"
	pagerandoLdFlags = "-Wl,--plugin-opt,pagerando"
)

type PagerandoProperties struct {
	Pagerando      *bool    `android:"arch_variant"`
}

type pagerando struct {
	Properties PagerandoProperties
}

func (pagerando *pagerando) props() []interface{} {
	return []interface{}{&pagerando.Properties}
}

func (pagerando *pagerando) begin(ctx BaseModuleContext) {
	// Pagerando should only be enabled for device builds
	if !ctx.Device() {
		pagerando.Properties.Pagerando = proptools.BoolPtr(false)
	}

	// Pagerando is currently only implemented for arm and arm64
	if ctx.Arch().ArchType != android.Arm && ctx.Arch().ArchType != android.Arm64 {
		pagerando.Properties.Pagerando = proptools.BoolPtr(false)
	}

	// Pagerando should only be enabled for libraries
	if !ctx.sharedLibrary() && !ctx.staticLibrary() {
		pagerando.Properties.Pagerando = proptools.BoolPtr(false)
	}

	// If local blueprint does not specify, allow global setting to enable
	// pagerando. Static libs should have both pagerando and non-pagerando
	// versions built for consumption by make.
	if ctx.sharedLibrary() && ctx.AConfig().Pagerando() &&
		pagerando.Properties.Pagerando == nil {
		pagerando.Properties.Pagerando = proptools.BoolPtr(true)
	}
}

func (pagerando *pagerando) deps(ctx BaseModuleContext, deps Deps) Deps {
	return deps
}

func (pagerando *pagerando) flags(ctx BaseModuleContext, flags Flags) Flags {
	if pagerando.Pagerando() {
		flags.CFlags = append(flags.CFlags, pagerandoCFlags)
		flags.LdFlags = append(flags.LdFlags, pagerandoLdFlags)
	}
	return flags
}

func (pagerando *pagerando) AndroidMk(ctx AndroidMkContext, ret *android.AndroidMkData) {
	ret.Extra = append(ret.Extra, func(w io.Writer, outputFile android.Path) {
		if pagerando.Properties.Pagerando == nil {
			return
		} else if pagerando.Pagerando() {
			fmt.Fprintln(w, "LOCAL_PAGERANDO := true")
		} else {
			fmt.Fprintln(w, "LOCAL_PAGERANDO := false")
		}
	})
}

func (pagerando *pagerando) Pagerando() bool {
	if pagerando == nil {
		return false
	}

	return Bool(pagerando.Properties.Pagerando)
}

// Create pagerando variants for modules that need them
func pagerandoMutator(mctx android.BottomUpMutatorContext) {
	if c, ok := mctx.Module().(*Module); ok && c.pagerando != nil {
		if c.pagerando.Pagerando() {
			mctx.SetDependencyVariation("pagerando")
			if c.lto == nil {
				mctx.ModuleErrorf("does not support LTO")
				return
			}
			c.lto.Properties.Lto = proptools.BoolPtr(true)
		} else if c.pagerando.Properties.Pagerando == nil &&
			mctx.AConfig().Pagerando() {
			modules := mctx.CreateVariations("", "pagerando")
			modules[0].(*Module).pagerando.Properties.Pagerando = proptools.BoolPtr(false)
			modules[1].(*Module).pagerando.Properties.Pagerando = proptools.BoolPtr(true)
			modules[1].(*Module).Properties.PreventInstall = true
			if modules[1].(*Module).lto == nil {
				mctx.ModuleErrorf("does not support LTO")
				return
			}
			modules[1].(*Module).lto.Properties.Lto = proptools.BoolPtr(true)
		}
	}
}
