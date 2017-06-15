// Copyright 2017 Immunant Inc. All rights reserved.
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

	"github.com/google/blueprint"

	"android/soong/android"
)

const (
	pagerandoCFlags = "-fpip"
	pagerandoLdFlags = "-Wl,--plugin-opt,pip"
)

type PagerandoProperties struct {
	Pagerando      *bool    `android:"arch_variant"`
	PagerandoDep   bool     `blueprint:"mutated"`
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
		pagerando.Properties.Pagerando = boolPtr(false)
	}

	// Pagerando only works for arm32 right now
	if ctx.Arch().ArchType != android.Arm {
		pagerando.Properties.Pagerando = boolPtr(false)
	}

	if !ctx.sharedLibrary() {
		return;
	}

	// If local blueprint does not specify, allow global setting to enable
	// pagerando
	if ctx.AConfig().EnablePagerando() && pagerando.Properties.Pagerando == nil {
		pagerando.Properties.Pagerando = boolPtr(true)
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

func (pagerando *pagerando) Pagerando() bool {
	if pagerando == nil {
		return false
	}

	return Bool(pagerando.Properties.Pagerando)
}

// Propagate pagerando requirements down from binaries
func pagerandoDepsMutator(mctx android.TopDownMutatorContext) {
	if c, ok := mctx.Module().(*Module); ok && c.pagerando.Pagerando() {
		mctx.VisitDepsDepthFirst(func(m blueprint.Module) {
			tag := mctx.OtherModuleDependencyTag(m)
			switch tag {
			case staticDepTag, staticExportDepTag, lateStaticDepTag, wholeStaticDepTag, objDepTag, reuseObjTag:
				if cc, ok := m.(*Module); ok && cc.pagerando != nil {
					if cc.pagerando.Properties.Pagerando == nil {
						cc.pagerando.Properties.PagerandoDep = true
					}
				}
			}
		})
	}
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
			c.lto.Properties.Lto = boolPtr(true)
		} else if c.pagerando.Properties.PagerandoDep {
			modules := mctx.CreateVariations("", "pagerando")
			modules[0].(*Module).pagerando.Properties.Pagerando = boolPtr(false)
			modules[1].(*Module).pagerando.Properties.Pagerando = boolPtr(true)
			modules[0].(*Module).pagerando.Properties.PagerandoDep = false
			modules[1].(*Module).pagerando.Properties.PagerandoDep = false
			modules[1].(*Module).Properties.PreventInstall = true
			if mctx.AConfig().EmbeddedInMake() {
				modules[1].(*Module).Properties.HideFromMake = true
			}
			if modules[1].(*Module).lto == nil {
				mctx.ModuleErrorf("does not support LTO")
				return
			}
			modules[1].(*Module).lto.Properties.Lto = boolPtr(true)
		}
		c.pagerando.Properties.PagerandoDep = false
	}
}
