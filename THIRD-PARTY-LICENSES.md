# Third-Party Notices

3270Web bundles and depends on software written by other people. This file
records what that software is, who wrote it, and the terms it comes under.

## s3270 — the x3270 family of 3270 terminal emulators

3270Web does not implement TN3270 itself. Every screen it renders, every field
it fills, and every AID key it sends is carried by **s3270**, the scripting
member of the **x3270** family of 3270 terminal emulators. 3270Web runs s3270
as a child process and translates its screen buffer into HTML.

That means the exacting part of this project — three decades of faithful 3270
and TN3270 protocol work, EBCDIC code pages, field attributes, structured
fields, TLS negotiation — was done by Paul Mattes and the x3270 contributors,
and given away for anyone to build on. 3270Web exists because of it. Our
thanks to them.

- Project home and documentation: <https://x3270.miraheze.org/wiki/Main_Page>
- Source: <https://github.com/pmattes/x3270>
- Licence: <https://github.com/pmattes/x3270/blob/master/LICENSE.md>

### How 3270Web obtains s3270

| Distribution | Source of the binary |
|---|---|
| Native binary / Windows build | `s3270-bin/` in this repository (currently s3270 4.5ga5) |
| Docker image (`ghcr.io/3270io/3270web`) | The `s3270` package installed from the Debian archive at image build time |

s3270 is used unmodified, as a separate executable invoked over its scripting
interface. 3270Web does not link against or embed x3270 source code.

### Licence — BSD 3-Clause

The following is reproduced verbatim from the x3270 distribution, in
satisfaction of the requirement that binary redistributions reproduce the
copyright notice, the list of conditions, and the disclaimer in the
accompanying documentation.

```
Copyright (c) 1993-2026 Paul Mattes.
Copyright (c) 2004-2005 Don Russell.
Copyright (c) 2004 Dick Altenbern.
Copyright (c) 1990 Jeff Sparkes.
Copyright (c) 1989 Georgia Tech Research Corporation (GTRC), Atlanta, GA
 30332.
All rights reserved.

Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions are met:
- Redistributions of source code must retain the above copyright
      notice, this list of conditions and the following disclaimer.
- Redistributions in binary form must reproduce the above copyright
      notice, this list of conditions and the following disclaimer in the
      documentation and/or other materials provided with the distribution.
- Neither the names of Paul Mattes, Don Russell, Dick Altenbern, Jeff
      Sparkes, GTRC nor the names of their contributors may be used to endorse
      or promote products derived from this software without specific prior
      written permission.

THIS SOFTWARE IS PROVIDED BY PAUL MATTES, DON RUSSELL, JEFF SPARKES, DICK
ALTENBERN AND GTRC "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING,
BUT NOT LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A
PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL PAUL MATTES, DON RUSSELL,
DICK ALTENBERN, JEFF SPARKES OR GTRC BE LIABLE FOR ANY DIRECT, INDIRECT,
INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE, DATA, OR
PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY THEORY OF
LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT (INCLUDING NEGLIGENCE
OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE OF THIS SOFTWARE, EVEN IF
ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
```

### On the third clause

The licence's third clause reserves the authors' names: they may not be used to
endorse or promote products derived from the software without prior written
permission. Nothing here is offered as an endorsement. The x3270 authors have no
involvement in 3270Web and have not reviewed, approved or promoted it. Naming them
above is a statement of what this software uses, which the licence requires, and
our thanks for it.
