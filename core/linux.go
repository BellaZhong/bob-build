/*
 * Copyright 2018 Arm Limited.
 * SPDX-License-Identifier: Apache-2.0
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

package core

import (
	"errors"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/google/blueprint"

	"github.com/ARM-software/bob-build/utils"
)

var (
	builddirVar = pctx.StaticVariable("BuildDir", builddir)
	srcdirVar   = pctx.StaticVariable("SrcDir", srcdir)
	pctx        = blueprint.NewPackageContext("bob")
)

type linuxGenerator struct {
	toolchainSet
}

// Convert a path to a library into a compiler flag.
// This needs to strip any path, file extension, lib prefix, and prepend -l
func pathToLibFlag(path string) string {
	_, base := filepath.Split(path)
	ext := filepath.Ext(base)
	base = strings.TrimSuffix(base, ext)
	base = strings.TrimPrefix(base, "lib")
	return "-l" + base
}

func addPhony(p phonyInterface, ctx blueprint.ModuleContext,
	installDeps []string, optional bool) {

	deps := append(p.outputs(getBackend(ctx)), installDeps...)

	ctx.Build(pctx,
		blueprint.BuildParams{
			Rule:     blueprint.Phony,
			Inputs:   deps,
			Outputs:  []string{p.shortName()},
			Optional: optional,
		})
}

func (g *linuxGenerator) sourcePrefix() string {
	return "$SrcDir"
}

func (g *linuxGenerator) buildDir() string {
	return "$BuildDir"
}

func (g *linuxGenerator) sourceOutputDir(m *generateCommon) string {
	return filepath.Join("$BuildDir", "gen", m.Name())
}

var copyRule = pctx.StaticRule("copy",
	blueprint.RuleParams{
		Command:     "cp --reflink=auto $in $out",
		Description: "$out",
	})

type targetableModule interface {
	blueprint.Module
	getTarget() string
}

// Where to put generated shared libraries to simplify linking
// As long as the module is targetable, we can infer the library path
func getSharedLibLinkPath(t targetableModule) string {
	return filepath.Join("$BuildDir", t.getTarget(), "shared", t.Name()+".so")
}

// Where to put generated binaries in order to make sure generated binaries
// are available in the same directory as compiled binaries
func getBinaryPath(t targetableModule) string {
	return filepath.Join("$BuildDir", t.getTarget(), "executable", t.Name())
}

// Generate the build actions for a generateSource module and populates the outputs.
func (g *linuxGenerator) generateCommonActions(m *generateCommon, ctx blueprint.ModuleContext, inouts []inout) {

	cmd, args, implicits, hostTarget := m.getArgs(ctx)

	ldLibraryPath := ""
	if _, ok := args["host_bin"]; ok {
		ldLibraryPath += "LD_LIBRARY_PATH=" + filepath.Join("${BuildDir}", hostTarget, "shared") + ":$$LD_LIBRARY_PATH "
	}
	utils.StripUnusedArgs(args, cmd)
	args["depfile"] = ""

	var pool blueprint.Pool
	if m.Properties.Console {
		// Console can be used to run longrunning jobs (even interactive jobs).
		pool = blueprint.Console
	}

	rulename := "gen_" + m.Name()
	//print("Keys:" + strings.Join(argkeys, ",") + "\n")
	rule := ctx.Rule(pctx,
		rulename,
		blueprint.RuleParams{
			Command: ldLibraryPath + cmd,
			// Restat is always set to true. This is due to wanting to enable scripts
			// to only update the outputs if they have changed (keeping the same mtime if it
			// has not). If there are no updates, the following rules will not have to update
			// the output.
			Restat:      true,
			Pool:        pool,
			Depfile:     "$depfile",
			Description: "$out",
		},
		utils.SortedKeys(args)...)

	for _, inout := range inouts {
		if _, ok := args["headers_generated"]; ok {
			headers := utils.Filter(inout.out, utils.IsHeader)
			args["headers_generated"] = strings.Join(headers, " ")
		}
		if _, ok := args["srcs_generated"]; ok {
			sources := utils.Filter(inout.out, utils.IsSource)
			args["srcs_generated"] = strings.Join(sources, " ")
		}

		args["depfile"] = inout.depfile
		implicits = append(implicits, inout.implicitSrcs...)

		ctx.Build(pctx,
			blueprint.BuildParams{
				Rule:      rule,
				Inputs:    append(inout.srcIn, inout.genIn...),
				Outputs:   inout.out,
				Implicits: implicits,
				Args:      args,
				Optional:  true,
			})
	}
}

func (g *linuxGenerator) generateSourceActions(m *generateSource, ctx blueprint.ModuleContext, inouts []inout) {
	g.generateCommonActions(&m.generateCommon, ctx, inouts)

	installDeps := g.install(m, ctx)
	addPhony(m, ctx, installDeps, !isBuiltByDefault(m))
}

func (g *linuxGenerator) transformSourceActions(m *transformSource, ctx blueprint.ModuleContext, inouts []inout) {
	g.generateCommonActions(&m.generateCommon, ctx, inouts)

	installDeps := g.install(m, ctx)
	addPhony(m, ctx, installDeps, !isBuiltByDefault(m))
}

func (g *linuxGenerator) genStaticActions(m *generateStaticLibrary, ctx blueprint.ModuleContext, inouts []inout) {
	g.generateCommonActions(&m.generateCommon, ctx, inouts)

	installDeps := g.install(m, ctx)
	addPhony(m, ctx, installDeps, !isBuiltByDefault(m))
}

func (g *linuxGenerator) genSharedActions(m *generateSharedLibrary, ctx blueprint.ModuleContext, inouts []inout) {
	g.generateCommonActions(&m.generateCommon, ctx, inouts)

	// Create a rule to copy the generated library
	// from gen_dir to the common library directory
	ctx.Build(pctx,
		blueprint.BuildParams{
			Rule:     copyRule,
			Inputs:   []string{getLibraryGeneratedPath(m, g)},
			Outputs:  []string{getSharedLibLinkPath(m)},
			Optional: true,
		})

	installDeps := g.install(m, ctx)
	addPhony(m, ctx, installDeps, !isBuiltByDefault(m))
}

func (g *linuxGenerator) genBinaryActions(m *generateBinary, ctx blueprint.ModuleContext, inouts []inout) {
	g.generateCommonActions(&m.generateCommon, ctx, inouts)

	// Create a rule to copy the generated binary
	// from gen_dir to the common binary directory
	ctx.Build(pctx,
		blueprint.BuildParams{
			Rule:     copyRule,
			Inputs:   []string{getLibraryGeneratedPath(m, g)},
			Outputs:  []string{getBinaryPath(m)},
			Optional: true,
		})

	installDeps := g.install(m, ctx)
	addPhony(m, ctx, installDeps, !isBuiltByDefault(m))
}

var asRule = pctx.StaticRule("as",
	blueprint.RuleParams{
		Depfile:     "$out.d",
		Deps:        blueprint.DepsGCC,
		Command:     "$build_wrapper $ascompiler $asflags $in -MD $out.d -o $out",
		Description: "$out",
	}, "ascompiler", "asflags", "build_wrapper")

var ccRule = pctx.StaticRule("cc",
	blueprint.RuleParams{
		Depfile:     "$out.d",
		Deps:        blueprint.DepsGCC,
		Command:     "$build_wrapper $ccompiler -c $cflags $conlyflags -MMD -MF $out.d $in -o $out",
		Description: "$out",
	}, "ccompiler", "cflags", "conlyflags", "build_wrapper")

var cxxRule = pctx.StaticRule("cxx",
	blueprint.RuleParams{
		Depfile:     "$out.d",
		Deps:        blueprint.DepsGCC,
		Command:     "$build_wrapper $cxxcompiler -c $cflags $cxxflags -MMD -MF $out.d $in -o $out",
		Description: "$out",
	}, "cxxcompiler", "cflags", "cxxflags", "build_wrapper")

func (l *library) ObjDir() string {
	return filepath.Join("${BuildDir}", l.Properties.TargetType, "objects", l.Name()) + string(os.PathSeparator)
}

// This function has common support to compile objs for static libs, shared libs and binaries.
func (l *library) CompileObjs(ctx blueprint.ModuleContext) []string {
	g := getBackend(ctx)
	srcs := l.GetSrcs(ctx)

	exportedIncludes, exportedCflags := l.GetExportedVariables(ctx)
	// There are 2 sets of include dirs - "global" and "local".
	// Local acts on the root source directory.

	// The order we want is  local_include_dirs, export_local_include_dirs,
	//                       include_dirs, export_include_dirs
	includeDirs := l.Properties.Local_include_dirs
	includeDirs = append(includeDirs, l.Properties.Export_local_include_dirs...)
	includeDirs = append(includeDirs, l.Properties.Include_dirs...)
	includeDirs = append(includeDirs, l.Properties.Export_include_dirs...)
	includeDirs = append(includeDirs, exportedIncludes...)

	gendirs, orderOnly := l.GetGeneratedHeaders(ctx)
	includeDirs = append(includeDirs, gendirs...)
	includeFlags := utils.PrefixAll(includeDirs, "-I")
	cflagsList := append(l.Properties.Cflags, includeFlags...)
	cflagsList = append(cflagsList, l.Properties.Export_cflags...)
	cflagsList = append(cflagsList, exportedCflags...)

	tc := g.getToolchain(l.Properties.TargetType)
	as, astargetflags := tc.getAssembler()
	cc, cctargetflags := tc.getCCompiler()
	cxx, cxxtargetflags := tc.getCXXCompiler()

	ctx.Variable(pctx, "asflags", strings.Join(append(astargetflags, l.Properties.Asflags...), " "))
	ctx.Variable(pctx, "cflags", strings.Join(cflagsList, " "))
	ctx.Variable(pctx, "conlyflags", strings.Join(append(cctargetflags, l.Properties.Conlyflags...), " "))
	ctx.Variable(pctx, "cxxflags", strings.Join(append(cxxtargetflags, l.Properties.Cxxflags...), " "))

	var objectFiles []string
	for _, source := range srcs {
		var rule blueprint.Rule
		args := make(map[string]string)
		switch path.Ext(source) {
		case ".s":
			args["ascompiler"] = as
			args["asflags"] = "$asflags"
			rule = asRule
		case ".S":
			// Assembly with .S suffix must be preprocessed by the C compiler
			fallthrough
		case ".c":
			args["ccompiler"] = cc
			args["cflags"] = "$cflags"
			args["conlyflags"] = "$conlyflags"
			rule = ccRule
		case ".cc":
			fallthrough
		case ".cpp":
			args["cxxcompiler"] = cxx
			args["cflags"] = "$cflags"
			args["cxxflags"] = "$cxxflags"
			rule = cxxRule
		default:
			panic(errors.New("Files with extension '" + path.Ext(source) + "' not supported"))
		}

		buildWrapper, buildWrapperDeps := l.Properties.Build.getBuildWrapperAndDeps(ctx)
		args["build_wrapper"] = buildWrapper

		sourceWithoutBuildDir := source
		if buildDir := g.buildDir(); strings.HasPrefix(sourceWithoutBuildDir, buildDir) {
			sourceWithoutBuildDir = sourceWithoutBuildDir[len(buildDir):]
		}
		var output = l.ObjDir() + sourceWithoutBuildDir + ".o"

		ctx.Build(pctx,
			blueprint.BuildParams{
				Rule:      rule,
				Outputs:   []string{output},
				Inputs:    []string{source},
				Args:      args,
				OrderOnly: append(orderOnly, buildWrapperDeps...),
				Optional:  true,
			})
		objectFiles = append(objectFiles, output)
	}

	return objectFiles
}

// Returns all the source files for a C/C++ library. This includes any sources that are generated.
func (l *library) GetSrcs(ctx blueprint.ModuleContext) []string {
	g := getBackend(ctx)
	srcs := l.Properties.GetSrcs(ctx)
	srcs = append(srcs, l.Properties.Build.SourceProps.Specials...)
	ctx.VisitDirectDepsIf(
		func(m blueprint.Module) bool { return ctx.OtherModuleDependencyTag(m) == generatedSourceTag },
		func(m blueprint.Module) {
			if gs, ok := m.(dependentInterface); ok {
				srcs = append(srcs, getSourcesGenerated(g, gs)...)
			} else {
				panic(errors.New(ctx.OtherModuleName(m) + " does not have outputs"))
			}
		})
	return srcs
}

// Returns the whole static dependencies for a library.
// Note that this is transitive, so it includes both the static library and all
// its whole static dependencies.
// While this is a break with the design idea of not having transitive dependencies,
// this is done due to the fact that merging two static libraries are not that easy.
func (l *library) GetWholeStaticLibs(ctx blueprint.ModuleContext) []string {
	g := getBackend(ctx)
	libs := []string{}
	ctx.VisitDirectDepsIf(
		func(m blueprint.Module) bool { return ctx.OtherModuleDependencyTag(m) == wholeStaticDepTag },
		func(m blueprint.Module) {
			if sl, ok := m.(*staticLibrary); ok {
				libs = append(libs, sl.outputs(g)...)
			} else if sl, ok := m.(*generateStaticLibrary); ok {
				libs = append(libs, getLibraryGeneratedPath(sl, g))
			} else {
				panic(errors.New(ctx.OtherModuleName(m) + " is not a static library"))
			}
		})

	return libs
}

// Returns all the static library dependencies for a module.
func (l *library) GetStaticLibs(ctx blueprint.ModuleContext) []string {
	g := getBackend(ctx)
	libs := []string{}
	for _, moduleName := range l.Properties.ResolvedStaticLibs {
		dep, _ := ctx.GetDirectDep(moduleName)
		if sl, ok := dep.(*staticLibrary); ok {
			libs = append(libs, sl.outputs(g)...)
		} else if sl, ok := dep.(*generateStaticLibrary); ok {
			libs = append(libs, getLibraryGeneratedPath(sl, g))
		} else {
			panic(errors.New(ctx.OtherModuleName(dep) + " is not a static library"))
		}
	}

	return libs
}

func (g *linuxGenerator) staticLibOutputDir(m *staticLibrary) string {
	return filepath.Join("${BuildDir}", m.Properties.TargetType, "static")
}

// The rule for building a static library
// Note that we need to remove the old library, else we will not remove the old object files
var staticLibraryRule = pctx.StaticRule("static_library",
	blueprint.RuleParams{
		Command:     "rm -f $out && $build_wrapper $ar -rcs $out $in",
		Description: "$out",
	}, "ar", "build_wrapper")

var wholeStaticLibraryRule = pctx.StaticRule("whole_static_library",
	blueprint.RuleParams{
		Command: "rm -f $out && { " +
			"echo create $out; " +
			"for i in $in; do echo addmod $$i; done; " +
			"for i in $whole_static_libs; do echo addlib $$i; done; " +
			"echo save; echo end; } " +
			"| $build_wrapper $ar -M && test -f $out",
		Description: "$out",
	}, "ar", "build_wrapper", "whole_static_libs")

func (g *linuxGenerator) staticActions(m *staticLibrary, ctx blueprint.ModuleContext) {
	if len(m.Properties.Static_libs) > 0 || len(m.Properties.Shared_libs) > 0 {
		panic(errors.New("static library cannot include another library, only include it using whole_static"))
	}

	rule := staticLibraryRule

	buildWrapper, buildWrapperDeps := m.Properties.Build.getBuildWrapperAndDeps(ctx)

	tc := g.getToolchain(m.Properties.TargetType)
	arBinary, _ := tc.getArchiver()

	args := map[string]string{
		"ar":            arBinary,
		"build_wrapper": buildWrapper,
	}

	wholeStaticLibs := m.library.GetWholeStaticLibs(ctx)

	if len(wholeStaticLibs) > 0 {
		rule = wholeStaticLibraryRule
		args["whole_static_libs"] = strings.Join(wholeStaticLibs, " ")
	}

	ctx.Build(pctx,
		blueprint.BuildParams{
			Rule:      rule,
			Outputs:   m.outputs(g),
			Inputs:    m.library.CompileObjs(ctx),
			Implicits: wholeStaticLibs,
			OrderOnly: buildWrapperDeps,
			Optional:  true,
			Args:      args,
		})

	installDeps := g.install(m, ctx)
	addPhony(m, ctx, installDeps, !isBuiltByDefault(m))
}

// This section contains functions that are common for shared libraries and executables.

func (l *library) getSharedLibLinkPaths(ctx blueprint.ModuleContext) (libs []string) {
	ctx.VisitDirectDepsIf(
		func(m blueprint.Module) bool { return ctx.OtherModuleDependencyTag(m) == sharedDepTag },
		func(m blueprint.Module) {
			if t, ok := m.(targetableModule); ok {
				libs = append(libs, getSharedLibLinkPath(t))

			} else {
				panic(errors.New(ctx.OtherModuleName(m) + " doesn't support targets"))
			}
		})
	return
}

func (l *library) getSharedLibFlags(ctx blueprint.ModuleContext) (flags []string) {
	// With forwarding shared library we do not have to use --no-as-needed for dependencies because it is already set
	useNoAsNeeded := !l.build().isForwardingSharedLibrary()

	ctx.VisitDirectDepsIf(
		func(m blueprint.Module) bool { return ctx.OtherModuleDependencyTag(m) == sharedDepTag },
		func(m blueprint.Module) {
			if sl, ok := m.(*sharedLibrary); ok {
				b := sl.build()
				if b.isForwardingSharedLibrary() {
					flags = append(flags, "-Wl,--copy-dt-needed-entries")
					if useNoAsNeeded {
						flags = append(flags, "-Wl,--no-as-needed")
					}
				}
				flags = append(flags, pathToLibFlag(sl.Name()))
				if b.isForwardingSharedLibrary() {
					if useNoAsNeeded {
						flags = append(flags, "-Wl,--as-needed")
					}
					flags = append(flags, "-Wl,--no-copy-dt-needed-entries")
				}
			} else if sl, ok := m.(*generateSharedLibrary); ok {
				flags = append(flags, pathToLibFlag(sl.Name()))
			} else {
				panic(errors.New(ctx.OtherModuleName(m) + " is not a shared library"))
			}
		})
	return
}

func (l *library) getSharedLibraryDir() string {
	return filepath.Join("${BuildDir}", l.Properties.TargetType, "shared")
}

func (g *linuxGenerator) sharedLibOutputDir(m *sharedLibrary) string {
	return m.library.getSharedLibraryDir()
}

func (g *linuxGenerator) sharedLibsDir(targetType string) string {
	return filepath.Join("${BuildDir}", targetType, "shared")
}

func (l *library) getCommonLibArgs(ctx blueprint.ModuleContext) map[string]string {
	ldflags := l.Properties.Ldflags

	if l.build().isForwardingSharedLibrary() {
		ldflags = append(ldflags, "-Wl,--no-as-needed")
	} else {
		ldflags = append(ldflags, "-Wl,--as-needed")
	}

	sharedLibFlags := l.getSharedLibFlags(ctx)

	tc := getBackend(ctx).getToolchain(l.Properties.TargetType)
	compiler, cctargetflags := tc.getCXXCompiler()
	buildWrapper, _ := l.Properties.Build.getBuildWrapperAndDeps(ctx)

	args := map[string]string{
		"build_wrapper":     buildWrapper,
		"compiler":          compiler,
		"ldflags":           strings.Join(append(cctargetflags, ldflags...), " "),
		"shared_libs_dir":   l.getSharedLibraryDir(),
		"shared_libs_flags": strings.Join(sharedLibFlags, " "),
		"static_libs":       strings.Join(l.GetStaticLibs(ctx), " "),
		"ldlibs":            strings.Join(l.Properties.Ldlibs, " "),
		"whole_static_libs": strings.Join(l.GetWholeStaticLibs(ctx), " "),
	}
	return args
}

func (l *sharedLibrary) getLibArgs(ctx blueprint.ModuleContext) map[string]string {
	args := l.getCommonLibArgs(ctx)
	ldflags := []string{}

	if l.Properties.Library_version != "" {
		var sonameFlag = "-Wl,-soname," + l.getSoname()
		ldflags = append(ldflags, sonameFlag)
	}

	args["ldflags"] += " " + strings.Join(ldflags, " ")

	return args
}

func (b *binary) getLibArgs(ctx blueprint.ModuleContext) map[string]string {
	return b.getCommonLibArgs(ctx)
}

// Returns the implicit dependencies for a library
func (l *library) Implicits(ctx blueprint.ModuleContext) []string {
	implicits := append(l.GetWholeStaticLibs(ctx), l.GetStaticLibs(ctx)...)
	implicits = append(implicits, l.getSharedLibLinkPaths(ctx)...)

	return implicits
}

// Return the parallelism that the current build environment can support
func getParallelism() int {
	// This environment variable is used to indicate available
	// parallelism in a certain build farm. It needs to be used in
	// preference to the detected number of CPUs
	if str, ok := os.LookupEnv("MPDTI_BUILD_PARALLELISM"); ok {
		if p, err := strconv.Atoi(str); err == nil {
			return p
		}
	}
	return runtime.NumCPU()
}

var parallelism = getParallelism()

var linkPoolParams = blueprint.PoolParams{
	Comment: "Limit the parallelization of linking, which is memory intensive",
	Depth:   (getParallelism() / 5) + 1,
}

var linkPool = pctx.StaticPool("link", linkPoolParams)

var sharedLibraryRule = pctx.StaticRule("shared_library",
	blueprint.RuleParams{
		Command: "$build_wrapper $compiler -shared $in -o $out $ldflags " +
			"-Wl,--whole-archive  $whole_static_libs -Wl,--no-whole-archive $static_libs " +
			"-Wl,-rpath-link,$shared_libs_dir -L$shared_libs_dir $shared_libs_flags $ldlibs",
		Description: "$out",
		Pool:        linkPool,
	}, "build_wrapper", "compiler", "ldflags", "ldlibs", "shared_libs_dir", "shared_libs_flags",
	"static_libs", "whole_static_libs")

var symlinkRule = pctx.StaticRule("symlink",
	blueprint.RuleParams{
		Command:     "for i in $out; do ln -nsf $target $$i; done;",
		Description: "$out",
	}, "target")

func (g *linuxGenerator) sharedActions(m *sharedLibrary, ctx blueprint.ModuleContext) {
	objectFiles := m.CompileObjs(ctx)

	_, buildWrapperDeps := m.Properties.Build.getBuildWrapperAndDeps(ctx)

	installDeps := g.install(m, ctx)

	// Create symlinks if needed
	for name, symlinkTgt := range m.librarySymlinks(ctx) {
		symlink := filepath.Join(m.outputDir(g), name)
		lib := filepath.Join(m.outputDir(g), symlinkTgt)
		ctx.Build(pctx,
			blueprint.BuildParams{
				Rule:     symlinkRule,
				Inputs:   []string{lib},
				Outputs:  []string{symlink},
				Args:     map[string]string{"target": symlinkTgt},
				Optional: true,
			})
		installDeps = append(installDeps, symlink)
	}

	ctx.Build(pctx,
		blueprint.BuildParams{
			Rule:      sharedLibraryRule,
			Outputs:   m.outputs(g),
			Inputs:    objectFiles,
			Implicits: m.library.Implicits(ctx),
			OrderOnly: buildWrapperDeps,
			Optional:  true,
			Args:      m.getLibArgs(ctx),
		})

	addPhony(m, ctx, installDeps, !isBuiltByDefault(m))
}

func (g *linuxGenerator) binaryOutputDir(m *binary) string {
	return filepath.Join("${BuildDir}", m.Properties.TargetType, "executable")
}

var executableRule = pctx.StaticRule("executable",
	blueprint.RuleParams{
		Command: "$build_wrapper $compiler $in -o $out $ldflags $static_libs " +
			"-Wl,-rpath-link,$shared_libs_dir -L$shared_libs_dir $shared_libs_flags $ldlibs",
		Description: "$out",
		Pool:        linkPool,
	}, "build_wrapper", "compiler", "ldflags", "ldlibs", "shared_libs_dir", "shared_libs_flags",
	"static_libs", "whole_static_libs")

func (g *linuxGenerator) binaryActions(m *binary, ctx blueprint.ModuleContext) {
	objectFiles := m.CompileObjs(ctx)
	/* By default, build all target binaries */
	optional := !isBuiltByDefault(m)

	_, buildWrapperDeps := m.Properties.Build.getBuildWrapperAndDeps(ctx)

	ctx.Build(pctx,
		blueprint.BuildParams{
			Rule:      executableRule,
			Outputs:   m.outputs(g),
			Inputs:    objectFiles,
			Implicits: m.library.Implicits(ctx),
			OrderOnly: buildWrapperDeps,
			Optional:  true,
			Args:      m.getLibArgs(ctx),
		})
	installDeps := g.install(m, ctx)
	addPhony(m, ctx, installDeps, optional)
}

func (*linuxGenerator) aliasActions(m *alias, ctx blueprint.ModuleContext) {
	srcs := []string{}

	/* Only depend on enabled targets */
	ctx.VisitDirectDepsIf(
		func(p blueprint.Module) bool { return ctx.OtherModuleDependencyTag(p) == aliasTag },
		func(p blueprint.Module) {
			if e, ok := p.(enableable); ok {
				if !isEnabled(e) {
					return
				}
			}
			srcs = append(srcs, ctx.OtherModuleName(p))
		})

	ctx.Build(pctx,
		blueprint.BuildParams{
			Rule:     blueprint.Phony,
			Inputs:   srcs,
			Outputs:  []string{m.Name()},
			Optional: true,
		})
}

var installRule = pctx.StaticRule("install",
	blueprint.RuleParams{
		Command:     "cp --remove-destination --reflink=auto $in $out",
		Description: "$out",
	})

func (*linuxGenerator) install(m interface{}, ctx blueprint.ModuleContext) []string {
	ins := m.(installable)

	installPath, ok := getInstallGroupPath(ctx)
	if !ok {
		return []string{}
	}
	installPath = filepath.Join("${BuildDir}", installPath)

	props := ins.getInstallableProps()
	if props.Relative_install_path != "" {
		installPath = filepath.Join(installPath, props.Relative_install_path)
	}

	installedFiles := []string{}

	rule := installRule
	args := map[string]string{}
	deps := []string{}
	if props.Post_install_cmd != "" {
		rulename := "install"
		tool := filepath.Join(ctx.ModuleDir(), props.Post_install_tool)

		// The command may or may not modify the output
		// --reflink=auto should work in either case.
		cmd := "cp --reflink=auto $in $out ; " + props.Post_install_cmd

		args["bob_config"] = configPath
		args["tool"] = tool
		utils.StripUnusedArgs(args, props.Post_install_cmd)

		rule = ctx.Rule(pctx,
			rulename,
			blueprint.RuleParams{
				Command:     cmd,
				Description: "$out",
			},
			utils.SortedKeys(args)...)
		deps = append(deps, tool)
	}

	for _, src := range ins.filesToInstall(ctx) {
		dest := filepath.Join(installPath, filepath.Base(src))

		ctx.Build(pctx,
			blueprint.BuildParams{
				Rule:      rule,
				Outputs:   []string{dest},
				Inputs:    []string{src},
				Args:      args,
				Implicits: deps,
				Optional:  true,
			})

		installedFiles = append(installedFiles, dest)
	}

	if symlinkIns, ok := m.(symlinkInstaller); ok {
		symlinks := symlinkIns.librarySymlinks(ctx)

		for key, value := range symlinks {
			symlink := filepath.Join(installPath, key)
			symlinkTgt := filepath.Join(installPath, value)
			ctx.Build(pctx,
				blueprint.BuildParams{
					Rule:     symlinkRule,
					Outputs:  []string{symlink},
					Inputs:   []string{symlinkTgt},
					Args:     map[string]string{"target": value},
					Optional: true,
				})

			installedFiles = append(installedFiles, symlink)
		}
	}

	return append(installedFiles, ins.getInstallDepPhonyNames(ctx)...)
}

func (g *linuxGenerator) resourceActions(m *resource, ctx blueprint.ModuleContext) {
	installDeps := g.install(m, ctx)
	addPhony(m, ctx, installDeps, false)
}

var copyIfExistsRule = pctx.StaticRule("cp_if_exists",
	blueprint.RuleParams{
		Command: "test -f $in && cp $in $out || (>&2 echo '" +
			"Warning: $in was not built - it has most likely " +
			"been disabled by your kernel config.' )",
		Description: "$out",
	})

func copyFileIfExists(ctx blueprint.ModuleContext, source string, dest string) {
	ctx.Build(pctx,
		blueprint.BuildParams{
			Rule:     copyIfExistsRule,
			Outputs:  []string{dest},
			Inputs:   []string{source},
			Optional: true,
		})
}

func (g *linuxGenerator) kernelModOutputDir(m *kernelModule) string {
	return filepath.Join("${BuildDir}", "target", "kernel_modules", m.Name())
}

var kbuildRule = pctx.StaticRule("kbuild",
	blueprint.RuleParams{
		Command: "python $kmod_build -o $out --depfile $out.d " +
			"--module-dir $output_module_dir $extra_includes " +
			"--sources $in $kbuild_extra_symbols " +
			"--kernel $kernel_dir --cross-compile '$kernel_compiler' " +
			"$kbuild_options --extra-cflags '$extra_cflags' $make_args",
		Depfile:     "$out.d",
		Deps:        blueprint.DepsGCC,
		Pool:        blueprint.Console,
		Description: "$out",
	}, "kmod_build", "extra_includes", "extra_cflags", "kbuild_extra_symbols", "kernel_dir",
	"kernel_compiler", "kbuild_options", "make_args", "output_module_dir")

func (g *linuxGenerator) kernelModuleActions(m *kernelModule, ctx blueprint.ModuleContext) {
	builtModule := filepath.Join(g.kernelModOutputDir(m), m.Name()+".ko")

	args := m.generateKbuildArgs(ctx)

	ctx.Build(pctx,
		blueprint.BuildParams{
			Rule:      kbuildRule,
			Outputs:   []string{builtModule},
			Inputs:    append(m.Properties.GetSrcs(ctx), m.Properties.Build.SourceProps.Specials...),
			Implicits: append(m.extraSymbolsFiles(ctx), args["copy_with_deps"]),
			Optional:  false,
			Args:      args,
		})

	// Add a dependency between Module.symvers and the kernel module. This
	// should really be added to Outputs or ImplicitOutputs above, but
	// Ninja doesn't support dependency files with multiple outputs yet.
	ctx.Build(pctx,
		blueprint.BuildParams{
			Rule:     blueprint.Phony,
			Inputs:   []string{builtModule},
			Outputs:  []string{filepath.Join(g.kernelModOutputDir(m), "Module.symvers")},
			Optional: true,
		})

	installDeps := g.install(m, ctx)
	addPhony(m, ctx, installDeps, false)
}

func (g *linuxGenerator) init(ctx *blueprint.Context, config *bobConfig) {
	g.toolchainSet.parseConfig(config)
}