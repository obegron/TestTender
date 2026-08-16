# Upstream provenance

TestTender is a derivative of
[joyrex2001/kubedock](https://github.com/joyrex2001/kubedock), distributed under
the MIT License.

The greenfield fork baseline was imported on 2026-08-16 from:

- upstream branch: `master`
- upstream commit: `9e43ee7cd9009aaef70dcc4871c86f1f13911fd4`
- upstream commit date: 2026-08-14

The original copyright and permission notice are retained in `LICENSE`.

## Updating from upstream

Configure the source repository as a read-only upstream remote:

```bash
git remote add upstream https://github.com/joyrex2001/kubedock.git
git fetch upstream
```

Review upstream changes from the recorded commit before merging. TestTender's
module path, product/resource identifiers, security policy packages and
deployment defaults intentionally differ from Kubedock and need conflict review.
