#!/bin/bash -e

BASEDIR="$(realpath "$(dirname "$0")/..")"

cd "$BASEDIR"

PATH=$(pwd)/dist:$PATH

if [[ -z "$1" ]]; then
	VERSION=$(<"$BASEDIR/VERSION")
else
	VERSION=$1
fi

echo "Modifying Capsule version to: $VERSION"

echo "$VERSION" > VERSION
