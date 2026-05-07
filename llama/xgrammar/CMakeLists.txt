# Builds the XGrammar C wrapper used by ollama's optional xgrammar
# sampler. Produces a single static archive:
#
#   libollama_xgrammar.a   (xgrammar core + ollama C wrapper)
#
# We bypass upstream xgrammar/CMakeLists.txt because it pulls in a
# Python-side tvm_ffi dependency that we do not want for the C ABI.
# Instead we compile cpp/*.cc (excluding tvm_ffi) directly into our
# wrapper archive.
#
# Configure & build out-of-tree:
#   cmake -S llama/xgrammar -B llama/xgrammar/build \
#       -DCMAKE_BUILD_TYPE=Release
#   cmake --build llama/xgrammar/build -j
#
# The Go wrapper (build tag `xgrammar`) links against the resulting
# archive. The default `go build` does not require this directory.
#
# NOTE: the cgo LDFLAGS in xgrammar.go assume an in-tree build dir at
# llama/xgrammar/build (i.e. `-B llama/xgrammar/build`). Out-of-tree
# build directories are not currently supported; the archive must end
# up at $SRCDIR/build/libollama_xgrammar.a relative to the Go file.

cmake_minimum_required(VERSION 3.18)
project(ollama_xgrammar LANGUAGES C CXX)

set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_POSITION_INDEPENDENT_CODE ON)

if(NOT CMAKE_BUILD_TYPE)
  set(CMAKE_BUILD_TYPE Release CACHE STRING "" FORCE)
endif()

set(XG_ROOT ${CMAKE_CURRENT_SOURCE_DIR}/xgrammar)

file(GLOB_RECURSE XG_SOURCES CONFIGURE_DEPENDS "${XG_ROOT}/cpp/*.cc")
list(FILTER XG_SOURCES EXCLUDE REGEX "${XG_ROOT}/cpp/tvm_ffi/.*\\.cc")

add_library(ollama_xgrammar STATIC
    ${XG_SOURCES}
    csrc/xgrammar_c.cc
)

target_include_directories(ollama_xgrammar
    PUBLIC ${CMAKE_CURRENT_SOURCE_DIR}
    PRIVATE
        ${XG_ROOT}/include
        ${XG_ROOT}/3rdparty/picojson
        ${XG_ROOT}/3rdparty/dlpack/include
)

if(MSVC)
  target_compile_options(ollama_xgrammar PRIVATE /W3)
else()
  target_compile_options(ollama_xgrammar PRIVATE
      -Wall -Wextra -Wno-unused-parameter -Wno-pedantic
  )
endif()
