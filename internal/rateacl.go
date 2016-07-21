// Copyright 2016 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package internal

import (
	"encoding/json"
	"reflect"

	"v.io/v23/context"
	"v.io/v23/security"
	"v.io/v23/security/access"

	"v.io/x/ref/lib/stats/counter"

	"messenger/ifc"
)

func RateAclFromJSON(b []byte) (acl RateAcl, err error) {
	if err = json.Unmarshal([]byte(b), &acl); err == nil {
		acl.Init()
	}
	return
}

type RateAcl []struct {
	Acl   access.AccessList `json:"acl"`
	Limit float32           `json:"limit"`
	c     *counter.Counter
}

func (r RateAcl) Init() {
	for i := range r {
		r[i].c = counter.New()
	}
}

func (r RateAcl) Authorize(ctx *context.T, call security.Call) error {
	if l, r := call.LocalBlessings().PublicKey(), call.RemoteBlessings().PublicKey(); l != nil && r != nil && reflect.DeepEqual(l, r) {
		return nil
	}
	blessings, invalid := security.RemoteBlessingNames(ctx, call)

	for i := range r {
		if !r[i].Acl.Includes(blessings...) {
			continue
		}
		if r[i].Limit == 0 {
			ctx.Infof("No permissions for %v matched by %v", r[i].Acl, blessings)
			break
		}
		if rate := r[i].c.Rate1m(); rate >= float64(r[i].Limit) {
			ctx.Infof("Rate limit exceeded for %v matched by %v, %f >= %f", r[i].Acl, blessings, rate, r[i].Limit)
			return ifc.NewErrRateLimitExceeded(ctx, r[i].Limit)
		}
		r[i].c.Incr(1)
		return nil
	}
	return access.NewErrNoPermissions(ctx, blessings, invalid, "")
}
