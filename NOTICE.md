This project is distributed under GPL-3.0; see LICENSE.

The five-protocol configuration and management workflow derives from
yonggekkk/sing-box-yg, GPL-3.0, commit b13f9435c4792fccf04d4fbc73e24a1948abff1b.
The OpenVPN profile validation derives from sarices/conduitvpn, GPL-3.0,
commit 4afc89bb9647fc1ccd8c240ab88e6e84e879398e.
The gateway's country selection, volunteer-node classification and SOCKS5
relay design adapt byJoey/fanout, commit d7ce5224caf3469b19876982abdf8d1be16c4a8b.
Its MIT license is reproduced below.

Subscription processing and client field mappings were reviewed against
sub-store-org/Sub-Store commit 7f2de2f33b1e7af6cf5e092e2d8d3d50b8eb1212
(MIT). This project contains independent Go implementations, not its JavaScript runtime.
YAML parsing links go.yaml.in/yaml/v3 v3.0.6 (MIT/Apache-2.0).
Its original LICENSE, NOTICE and Apache license are distributed in third_party/modules/.

The separate managed-core executable links SagerNet/sing-box v1.14.4
(GPL-3.0-or-later), with an independently implemented authenticated-stream
guard. Upstream source: https://github.com/SagerNet/sing-box/tree/v1.14.4.
The account ledger links modernc.org/sqlite v1.62.0 and its Go dependencies.
Exact module versions and checksums are recorded in go.mod and go.sum.
License notices for the full linked module graph on Linux and Windows, including
QR generation and WireGuard, are preserved in third_party/modules/.
These are free/open-source
dependencies; external hosting, domains and optional delivery services are separate.
No s-ui or 2s-ui implementation or frontend assets were copied into this project.

MIT License

Copyright (c) 2026 Joey (byJoey)

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
