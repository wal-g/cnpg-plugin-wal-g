#!/bin/bash
set -euxo pipefail

kind delete cluster -n cnpg-wal-g
