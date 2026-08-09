# Acknowledgements

## s3270 and the x3270 family

3270Web does not speak TN3270 itself. Every screen you see in the browser,
every field you type into, and every PF key you press is carried by **s3270** —
the scripting member of the **x3270** family of 3270 terminal emulators.
3270Web runs s3270 as a child process and renders its screen buffer as HTML.

Which is to say: the exacting part of this project — three decades of faithful
3270 and TN3270 protocol work, EBCDIC code pages, field attributes, structured
fields, TLS negotiation — was done by Paul Mattes and the x3270 contributors,
and given away for anyone to build on. 3270Web is a browser front end wrapped
around their work, and it would not exist without it. Our thanks to them.

If 3270Web is useful to you, the upstream project is worth knowing about in its
own right:

- [x3270 project home and documentation](https://x3270.miraheze.org/wiki/Main_Page)
- [Source on GitHub](https://github.com/pmattes/x3270)
- [Licence (BSD 3-Clause)](https://github.com/pmattes/x3270/blob/master/LICENSE.md)

### Licence

s3270 is distributed under a BSD 3-Clause licence, copyright Paul Mattes, Don
Russell, Dick Altenbern, Jeff Sparkes and the Georgia Tech Research Corporation.
3270Web uses it unmodified, as a separate executable driven over its scripting
interface.

The full licence text, together with a note on where each 3270Web distribution
obtains its s3270 binary, is reproduced in
[`THIRD-PARTY-NOTICES.md`](https://github.com/3270io/3270Web/blob/main/THIRD-PARTY-NOTICES.md)
in the repository.

!!! note "Not an endorsement"

    The x3270 authors do not endorse 3270Web. The credit above is attribution,
    and gratitude, and nothing more.
