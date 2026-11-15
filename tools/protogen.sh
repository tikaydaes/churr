#!/bin/sh
# Regenerates everything that comes from the protobuf schema (docs/WIRE-PROTOBUF.md):
#
#   1. tools/openapi-gen/build.py writes docs/openapi.yaml, proto/churr/v1/churr.proto, its number
#      lock, and the tables the Go and Dart codecs walk.
#   2. protoc writes the app's message classes (app/lib/core/wire/gen) and the descriptor set the
#      Go tests check the hand-written codec against (internal/wirepb/testdata/churr.binpb).
#   3. The Go tests rewrite the shared vectors under testdata/wirepb.
#   4. protoc writes the descriptor set of the link protocol's schema (proto/churr/link/v1), which
#      the link's codec is tested against (internal/link/wire/testdata/link.binpb).
#
# Everything it writes is checked in, so building and testing need none of these tools. Run it after
# changing tools/openapi-gen. Needs: python3 with PyYAML (PYTHON names another interpreter), protoc,
# and protoc-gen-dart (`dart pub global activate protoc_plugin`).
set -eu
cd "$(dirname "$0")/.."
PATH="$PATH:$HOME/.pub-cache/bin"
"${PYTHON:-python3}" tools/openapi-gen/build.py
mkdir -p app/lib/core/wire/gen internal/wirepb/testdata
protoc -I proto --dart_out=app/lib/core/wire/gen \
  --descriptor_set_out=internal/wirepb/testdata/churr.binpb churr/v1/churr.proto
# protoc mirrors the package path; the app keeps the two files it uses, flat.
mv app/lib/core/wire/gen/churr/v1/churr.pb.dart app/lib/core/wire/gen/churr/v1/churr.pbenum.dart \
   app/lib/core/wire/gen/
rm -rf app/lib/core/wire/gen/churr
go test ./internal/wirepb -run 'TestVectors' -update -count=1
mkdir -p internal/link/wire/testdata
protoc -I proto --include_imports --descriptor_set_out=internal/link/wire/testdata/link.binpb churr/link/v1/link.proto
