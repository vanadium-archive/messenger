#!/system/bin/sh

export TMPDIR=$(dirname $0)/tmp
mkdir -p $TMPDIR

./vmsg chat \
  --store-dir=$TMPDIR/store \
  --rate-acl-in='[{"acl":{"In":["..."]},"limit":20}]' \
  --rate-acl-out='[{"acl":{"In":["..."]},"limit":100}]' \
  --rate-acl-sender='[{"acl":{"In":["..."]},"limit":100}]'
