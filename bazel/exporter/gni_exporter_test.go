// Copyright 2022 Google LLC
//
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package exporter

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.skia.org/skia/bazel/exporter/build_proto/build"
	"go.skia.org/skia/bazel/exporter/interfaces/mocks"
	"google.golang.org/protobuf/proto"
)

// The expected gn/core.gni file contents for createCoreSourcesQueryResult().
// This expected result is handmade.
const publicSrcsExpectedGNI = `# DO NOT EDIT: This is a generated file.
# See //bazel/exporter_tool/README.md for more information.

_src = get_path_info("../src", "abspath")

skia_core_sources = [
  "$_src/core/SkAAClip.cpp",
  "$_src/core/SkAlphaRuns.cpp",
  "$_src/core/SkATrace.cpp",
  "$_src/opts/SkBitmapProcState_opts.h",
  "$_src/opts/SkBlitMask_opts.h",
  "$_src/opts/SkBlitRow_opts.h",
]

skia_core_sources += skia_pathops_sources
skia_core_sources += skia_skpicture_sources

skia_core_public += skia_pathops_public
skia_core_public += skia_skpicture_public
`

var exportDescs = []GNIExportDesc{
	{GNI: "gn/core.gni", Vars: []GNIFileListExportDesc{
		{Var: "skia_core_sources",
			Rules: []string{
				"//src/core:core_srcs",
				"//src/opts:private_hdrs",
			}}},
	},
}

var testExporterParams = GNIExporterParams{
	WorkspaceDir: "/path/to/workspace",
	ExportDescs:  exportDescs,
}

func createCoreSourcesQueryResult() *build.QueryResult {
	qr := build.QueryResult{}
	ruleDesc := build.Target_RULE

	// Rule #1
	srcs := []string{
		"//src/core:SkAAClip.cpp",
		"//src/core:SkATrace.cpp",
		"//src/core:SkAlphaRuns.cpp",
	}
	r1 := createTestBuildRule("//src/core:core_srcs", "filegroup",
		"/path/to/workspace/src/core/BUILD.bazel:376:20", srcs)
	t1 := build.Target{Rule: r1, Type: &ruleDesc}
	qr.Target = append(qr.Target, &t1)

	// Rule #2
	srcs = []string{
		"//src/opts:SkBitmapProcState_opts.h",
		"//src/opts:SkBlitMask_opts.h",
		"//src/opts:SkBlitRow_opts.h",
	}
	r2 := createTestBuildRule("//src/opts:private_hdrs", "filegroup",
		"/path/to/workspace/src/opts/BUILD.bazel:26:10", srcs)
	t2 := build.Target{Rule: r2, Type: &ruleDesc}
	qr.Target = append(qr.Target, &t2)
	return &qr
}

func TestGNIExporterExport_ValidInput_Success(t *testing.T) {
	qr := createCoreSourcesQueryResult()
	protoData, err := proto.Marshal(qr)
	require.NoError(t, err)

	fs := mocks.NewFileSystem(t)
	var contents bytes.Buffer
	fs.On("OpenFile", mock.Anything).Once().Run(func(args mock.Arguments) {
		path := args.String(0)
		assert.True(t, filepath.IsAbs(path))
		assert.Equal(t, "/path/to/workspace/gn/core.gni", filepath.ToSlash(path))
	}).Return(&contents, nil).Once()
	e := NewGNIExporter(testExporterParams, fs)
	qcmd := mocks.NewQueryCommand(t)
	qcmd.On("Read", mock.Anything).Return(protoData, nil).Once()
	err = e.Export(qcmd)
	require.NoError(t, err)

	assert.Equal(t, publicSrcsExpectedGNI, contents.String())
}

func TestGNIExporterCheckCurrent_CurrentData_ReturnZero(t *testing.T) {
	fs := mocks.NewFileSystem(t)
	fs.On("ReadFile", mock.Anything).Run(func(args mock.Arguments) {
		path := args.String(0)
		assert.True(t, filepath.IsAbs(path))
		assert.Equal(t, "/path/to/workspace/gn/core.gni", filepath.ToSlash(path))
	}).Return([]byte(publicSrcsExpectedGNI), nil)
	e := NewGNIExporter(testExporterParams, fs)
	qcmd := mocks.NewQueryCommand(t)
	var errBuff bytes.Buffer
	numOutOfDate, err := e.CheckCurrent(qcmd, &errBuff)
	os.Stdout.Write(errBuff.Bytes()) // Echo output messages to stdout.
	assert.NoError(t, err)
	assert.Zero(t, numOutOfDate)
}

func TestMakeRelativeFilePathForGNI_MatchingRootDir_Success(t *testing.T) {
	test := func(name, target, expectedPath string) {
		t.Run(name, func(t *testing.T) {
			path, err := makeRelativeFilePathForGNI(target)
			require.NoError(t, err)
			assert.Equal(t, expectedPath, path)
		})
	}

	test("src", "src/core/file.cpp", "$_src/core/file.cpp")
	test("include", "include/core/file.h", "$_include/core/file.h")
	test("modules", "modules/mod/file.cpp", "$_modules/mod/file.cpp")
}

func TestMakeRelativeFilePathForGNI_IndalidInput_ReturnError(t *testing.T) {
	test := func(name, target string) {
		t.Run(name, func(t *testing.T) {
			_, err := makeRelativeFilePathForGNI(target)
			assert.Error(t, err)
		})
	}

	test("EmptyString", "")
	test("UnsupportedRootDir", "//valid/rule/incorrect/root/dir:file.cpp")
}

func TestIsHeaderFile_HeaderFiles_ReturnTrue(t *testing.T) {
	test := func(name, path string) {
		t.Run(name, func(t *testing.T) {
			assert.True(t, isHeaderFile(path))
		})
	}

	test("LowerH", "path/to/file.h")
	test("UpperH", "path/to/file.H")
	test("MixedHpp", "path/to/file.Hpp")
}

func TestIsHeaderFile_NonHeaderFiles_ReturnTrue(t *testing.T) {
	test := func(name, path string) {
		t.Run(name, func(t *testing.T) {
			assert.False(t, isHeaderFile(path))
		})
	}

	test("EmptyString", "")
	test("DirPath", "/path/to/dir")
	test("C++Source", "/path/to/file.cpp")
	test("DotHInDir", "/path/to/dir.h/file.cpp")
	test("Go", "main.go")
}

func TestFileListContainsOnlyCppHeaderFiles_AllHeaders_ReturnsTrue(t *testing.T) {
	test := func(name string, paths []string) {
		t.Run(name, func(t *testing.T) {
			assert.True(t, fileListContainsOnlyCppHeaderFiles(paths))
		})
	}

	test("OneFile", []string{"file.h"})
	test("Multiple", []string{"file.h", "foo.hpp"})
}

func TestFileListContainsOnlyCppHeaderFiles_NotAllHeaders_ReturnsFalse(t *testing.T) {
	test := func(name string, paths []string) {
		t.Run(name, func(t *testing.T) {
			assert.False(t, fileListContainsOnlyCppHeaderFiles(paths))
		})
	}

	test("Nil", nil)
	test("HeaderFiles", []string{"file.h", "file2.cpp"})
	test("GoFile", []string{"file.go"})
}

func TestWorkspaceToAbsPath_ReturnsAbsolutePath(t *testing.T) {
	fs := mocks.NewFileSystem(t)
	e := NewGNIExporter(testExporterParams, fs)
	require.NotNil(t, e)

	test := func(name, input, expected string) {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, expected, e.workspaceToAbsPath(input))
		})
	}

	test("FileInDir", "foo/bar.txt", "/path/to/workspace/foo/bar.txt")
	test("DirInDir", "foo/bar", "/path/to/workspace/foo/bar")
	test("RootFile", "root.txt", "/path/to/workspace/root.txt")
	test("WorkspaceDir", "", "/path/to/workspace")
}

func TestGetGNILineVariable_LinesWithVariables_ReturnVariable(t *testing.T) {
	test := func(name, inputLine, expected string) {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, expected, getGNILineVariable(inputLine))
		})
	}

	test("EqualWithSpaces", `foo = [ "something" ]`, "foo")
	test("EqualNoSpaces", `foo=[ "something" ]`, "foo")
	test("EqualSpaceBefore", `foo =[ "something" ]`, "foo")
	test("MultilineList", `foo = [`, "foo")
}

func TestGetGNILineVariable_LinesWithVariables_NoMatch(t *testing.T) {
	test := func(name, inputLine, expected string) {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, expected, getGNILineVariable(inputLine))
		})
	}

	test("FirstCharSpace", ` foo = [ "something" ]`, "") // Impl. requires formatted file.
	test("NotList", `foo = bar`, "")
	test("ListLiteral", `[ "something" ]`, "")
	test("ListInComment", `# foo = [ "something" ]`, "")
	test("MissingVariable", `=[ "something" ]`, "")
	test("EmptyString", ``, "")
}

func TestFoo_DeprecatedFiles_ReturnsTrue(t *testing.T) {
	assert.True(t, isSourceFileDeprecated("include/core/SkDrawLooper.h"))
}

func TestFoo_NotDeprecatedFiles_ReturnsFalse(t *testing.T) {
	assert.False(t, isSourceFileDeprecated("include/core/SkColor.h"))
}

func TestExtractTopLevelFolder_PathsWithTopDir_ReturnsTopDir(t *testing.T) {
	test := func(name, input, expected string) {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, expected, extractTopLevelFolder(input))
		})
	}
	test("TopIsDir", "foo/bar/baz.txt", "foo")
	test("TopIsVariable", "$_src/bar/baz.txt", "$_src")
	test("TopIsFile", "baz.txt", "baz.txt")
	test("TopIsAbsDir", "/foo/bar/baz.txt", "")
}

func TestExtractTopLevelFolder_PathsWithNoTopDir_ReturnsEmptyString(t *testing.T) {
	test := func(name, input, expected string) {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, expected, extractTopLevelFolder(input))
		})
	}
	test("EmptyString", "", "")
	test("EmptyAbsRoot", "/", "")
	test("MultipleSlashes", "///", "")
}

func TestAddGNIVariablesToWorkspacePaths_ValidInput_ReturnsVariables(t *testing.T) {
	test := func(name string, inputPaths, expected []string) {
		t.Run(name, func(t *testing.T) {
			gniPaths, err := addGNIVariablesToWorkspacePaths(inputPaths)
			require.NoError(t, err)
			assert.Equal(t, expected, gniPaths)
		})
	}
	test("EmptySlice", nil, []string{})
	test("AllVariables",
		[]string{"src/include/foo.h",
			"include/foo.h",
			"modules/foo.cpp"},
		[]string{"$_src/include/foo.h",
			"$_include/foo.h",
			"$_modules/foo.cpp"})
}

func TestAddGNIVariablesToWorkspacePaths_InvalidInput_ReturnsError(t *testing.T) {
	test := func(name string, inputPaths []string) {
		t.Run(name, func(t *testing.T) {
			_, err := addGNIVariablesToWorkspacePaths(inputPaths)
			assert.Error(t, err)
		})
	}
	test("InvalidTopDir", []string{"nomatch/include/foo.h"})
	test("RuleNotPath", []string{"//src/core:source.cpp"})
}

func TestConvertTargetsToFilePaths_ValidInput_ReturnsPaths(t *testing.T) {
	test := func(name string, inputTargets, expected []string) {
		t.Run(name, func(t *testing.T) {
			paths, err := convertTargetsToFilePaths(inputTargets)
			require.NoError(t, err)
			assert.Equal(t, expected, paths)
		})
	}
	test("EmptySlice", nil, []string{})
	test("Files",
		[]string{"//src/include:foo.h",
			"//include:foo.h",
			"//modules:foo.cpp"},
		[]string{"src/include/foo.h",
			"include/foo.h",
			"modules/foo.cpp"})
}

func TestConvertTargetsToFilePaths_InvalidInput_ReturnsError(t *testing.T) {
	test := func(name string, inputTargets []string) {
		t.Run(name, func(t *testing.T) {
			_, err := convertTargetsToFilePaths(inputTargets)
			assert.Error(t, err)
		})
	}
	test("EmptyString", []string{""})
	test("ValidTargetEmptyString", []string{"//src/include:foo.h", ""})
	test("EmptyStringValidTarget", []string{"//src/include:foo.h", ""})
}

func TestFilterDeprecatedFiles_ContainsDeprecatedFiles_DeprecatedFiltered(t *testing.T) {
	test := func(name string, inputFiles, expected []string) {
		t.Run(name, func(t *testing.T) {
			paths := filterDeprecatedFiles(inputFiles)
			assert.Equal(t, expected, paths)
		})
	}
	test("OneDeprecated",
		[]string{"include/core/SkDrawLooper.h"},
		[]string{})
	test("MultipleDeprecated",
		[]string{
			"include/core/SkDrawLooper.h",
			"include/effects/SkBlurDrawLooper.h"},
		[]string{})
	test("FirstDeprecated",
		[]string{
			"include/core/SkDrawLooper.h",
			"not/deprecated/file.h"},
		[]string{"not/deprecated/file.h"})
	test("LastDeprecated",
		[]string{
			"not/deprecated/file.h",
			"include/core/SkDrawLooper.h"},
		[]string{"not/deprecated/file.h"})
}

func TestFilterDeprecatedFiles_NoDeprecatedFiles_SliceUnchanged(t *testing.T) {
	test := func(name string, inputFiles, expected []string) {
		t.Run(name, func(t *testing.T) {
			paths := filterDeprecatedFiles(inputFiles)
			assert.Equal(t, expected, paths)
		})
	}
	test("EmptySlice", nil, []string{})
	test("NoneDeprecated",
		[]string{
			"not/deprecated/file.h",
			"also/not/deprecated/file.h"},
		[]string{
			"not/deprecated/file.h",
			"also/not/deprecated/file.h"})
}

func TestFindDuplicate_ContainsDuplicate_ReturnsTrue(t *testing.T) {
	test := func(name string, files []string, expected string) {
		t.Run(name, func(t *testing.T) {
			path, hasDup := findDuplicate(files)
			assert.True(t, hasDup)
			assert.Equal(t, expected, path)
		})
	}
	test("AllDups",
		[]string{
			"path/to/file.h",
			"path/to/file.h"},
		"path/to/file.h")
	test("FirstFileDup",
		[]string{
			"path/to/file.h",
			"path/to/file.h",
			"path/to/non_dup.h"},
		"path/to/file.h")
	test("LastFileDup",
		[]string{
			"path/to/a_non_dup.h",
			"path/to/file.h",
			"path/to/file.h"},
		"path/to/file.h")
	test("CaseInsensitive",
		[]string{
			"path/to/file.h",
			"path/to/File.h"},
		"path/to/file.h")
}

func TestFindDuplicate_NoDuplicates_ReturnsFalse(t *testing.T) {
	test := func(name string, files []string) {
		t.Run(name, func(t *testing.T) {
			_, hasDup := findDuplicate(files)
			assert.False(t, hasDup)
		})
	}
	test("EmptySlice", []string{})
	test("nilSlice", nil)
	test("SingleFile",
		[]string{
			"path/to/file.h"})
	test("MultipleFiles",
		[]string{
			"path/to/file_a.h",
			"path/to/file_b.h",
			"path/to/file_c.h"})
}
