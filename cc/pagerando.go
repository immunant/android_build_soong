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
	"bufio"
	"fmt"
	"io"
	"os"

	"github.com/google/blueprint/proptools"

	"android/soong/android"
)

var (
	// pagerandoCFlags  = []string{}
	// pagerandoLdFlags = []string{}
	pagerandoCFlags  = []string{"-fsanitize=pagerando",
		"-fsanitize-blacklist=build/soong/cc/config/pagerando_blacklist.txt"}
	pagerandoLdFlags = []string{"-Wl,--plugin-opt,pagerando",
		"-Wl,--plugin-opt,-pagerando-skip-trivial",
		"-Wl,--plugin-opt,-pagerando-binning-strategy=pgo",
		// Force _LOCAL_PAGE_OFFSET_TABLE_ to be needed, so we don't
		// lose the POT section to --gc-sections
		"-Wl,--undefined,_LOCAL_PAGE_OFFSET_TABLE_"}
)

const (
	// This file stores the authoritative ordering of system libraries in
	// the page offset table (POT). It is used on-device by the dynamic
	// linker to place library POT pages.
	potIndexFilename = "system/core/rootdir/etc/ld.pot_map.txt"

	globalPOTIndexLdFlag = "-Wl,--plugin-opt,-global-pot-index=%d"
)

type PagerandoProperties struct {
	Pagerando    *bool  `android:"arch_variant"`
	PagerandoDep  bool  `blueprint:"mutated"`
	POTIndex     *int64 `blueprint:"mutated"`
}

type pagerando struct {
	Properties PagerandoProperties

	libraryPOTIndices map[string]int64
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
	// if !ctx.sharedLibrary() && !ctx.staticLibrary() {
	// 	pagerando.Properties.Pagerando = proptools.BoolPtr(false)
	// }

	// If we are building against the SDK, we can't link against ld-android.so
	// TODO: Fix this?
	if ctx.useSdk() {
		pagerando.Properties.Pagerando = proptools.BoolPtr(false)
	}

	// If local blueprint does not specify, allow global setting to enable
	// pagerando. Static libs should have both pagerando and non-pagerando
	// versions built for consumption by make.
	if ctx.AConfig().Pagerando() {
		if pagerando.Properties.Pagerando == nil {
			if ctx.sharedLibrary() {
				pagerando.Properties.Pagerando = proptools.BoolPtr(true)
			}
			if ctx.staticLibrary() {
				pagerando.Properties.PagerandoDep = true
			}
		}
	} else {
		if pagerando.Properties.Pagerando == nil {
			pagerando.Properties.Pagerando = proptools.BoolPtr(false)
		}
	}

	if pagerando.Pagerando() {
		// Lazily intialize POT map because we need a context for a file
		// path.
		if pagerando.libraryPOTIndices == nil {
			pagerando.readPOTIndices(ctx)
		}

		libName := ctx.ModuleName() + ctx.toolchain().ShlibSuffix()
		if index, ok := pagerando.libraryPOTIndices[libName]; ok {
			pagerando.Properties.POTIndex = proptools.Int64Ptr(index)
		}
	}
}

func (pagerando *pagerando) deps(ctx BaseModuleContext, deps Deps) Deps {
	// Pagerando requires the _PAGE_OFFSET_TABLE_ symbol which is defined
	// specially by the linker.
	if pagerando.Pagerando() && ctx.ModuleName() != "ld-android" {
		deps.SharedLibs = append(deps.SharedLibs, "ld-android")
	}
	return deps
}

func (pagerando *pagerando) flags(ctx BaseModuleContext, flags Flags) Flags {
	if pagerando.Pagerando() {
		flags.CFlags = append(flags.CFlags, pagerandoCFlags...)
		flags.LdFlags = append(flags.LdFlags, pagerandoLdFlags...)

		if pagerando.Properties.POTIndex != nil {
			flags.LdFlags = append(flags.LdFlags,
				fmt.Sprintf(globalPOTIndexLdFlag, *pagerando.Properties.POTIndex))
		}
	}
	return flags
}

func (pagerando *pagerando) AndroidMk(ctx AndroidMkContext, ret *android.AndroidMkData) {
	ret.Extra = append(ret.Extra, func(w io.Writer, outputFile android.Path) {
		if pagerando == nil {
			fmt.Fprintln(w, "LOCAL_PAGERANDO := false")
			return
		}
		if pagerando.Properties.Pagerando == nil {
			return
		} else if pagerando.Pagerando() {
			fmt.Fprintln(w, "LOCAL_PAGERANDO := true")
		} else {
			fmt.Fprintln(w, "LOCAL_PAGERANDO := false")
		}
	})
}

// Can be called with a null receiver
func (pagerando *pagerando) Pagerando() bool {
	if pagerando == nil {
		return false
	}

	return Bool(pagerando.Properties.Pagerando)
}

func (pagerando *pagerando) readPOTIndices(ctx BaseModuleContext) {
	potIndexPath := android.PathForSource(ctx, potIndexFilename)
	potIndexReader, err := os.Open(potIndexPath.String())
	defer potIndexReader.Close()
	if err != nil {
		panic(fmt.Errorf("Failed to open POT index file %s: %s", potIndexFilename, err))
	}

	pagerando.libraryPOTIndices = make(map[string]int64)

	scanner := bufio.NewScanner(potIndexReader)
	var curIndex int64 = 0
	for scanner.Scan() {
		pagerando.libraryPOTIndices[scanner.Text()] = curIndex
		curIndex++
	}
}

// // Propagate pagerando dependency requirements down from binaries
// func pagerandoDepsMutator(mctx android.TopDownMutatorContext) {
// 	if c, ok := mctx.Module().(*Module); ok && c.pagerando.Pagerando() {
// 		mctx.VisitDepsDepthFirst(func(module android.Module) {
// 			if d, ok := module.(*Module); ok && d.pagerando != nil &&
// 				d.pagerando.Properties.Pagerando != proptools.BoolPtr(false) &&
// 				d.static() {
// 				d.pagerando.Properties.PagerandoDep = true
// 			}
// 		})
// 	}
// }

// Create pagerando variants for modules that need them
func pagerandoMutator(mctx android.BottomUpMutatorContext) {
	if c, ok := mctx.Module().(*Module); ok && c.pagerando != nil {
		if c.pagerando.Pagerando() {
			mctx.SetDependencyVariation("pagerando")
			c.lto.EnableFull(mctx);
		} else if c.pagerando.Properties.PagerandoDep {
			if c.lto == nil || c.lto.Disabled() {
				// Do not build this module with pagerando since
				// LTO is disabled. This should not be a fatal
				// error.
				c.pagerando.Properties.Pagerando = proptools.BoolPtr(false)
				return
			}
			modules := mctx.CreateVariations("", "pagerando")
			modules[0].(*Module).pagerando.Properties.Pagerando = proptools.BoolPtr(false)
			modules[1].(*Module).pagerando.Properties.Pagerando = proptools.BoolPtr(true)
			modules[1].(*Module).Properties.PreventInstall = true
			modules[1].(*Module).lto.Properties.Lto.Full = proptools.BoolPtr(true)
		}
	}
}
