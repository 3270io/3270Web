# h3270 Java → Go Porting Plan

## 1) Scope and architecture summary
- This app is a Java webapp (Servlets/JSP) with a 3270 host core.
- Entry points:
  - Servlet: `org.h3270.web.Servlet`
  - Style servlet: `org.h3270.web.StyleServlet`
  - Optional portlet: `org.h3270.web.Portlet`
- Core 3270 logic:
  - Subprocess bridge: `org.h3270.host.S3270`
  - Screen parsing: `org.h3270.host.S3270Screen`
- Rendering pipeline:
  - `org.h3270.render.Engine` + `HtmlRenderer`, `RegexRenderer`, `TextRenderer`

## 2) Porting approach (incremental)
1. **Lock behavior with tests**
   - Add black‑box tests around HTTP endpoints and rendered output.
   - Start with GET/POST behavior for `/servlet`.

2. **Port the core library first**
   - `internal/host`: implement the `s3270` subprocess wrapper and screen parser.
   - `internal/render`: port the render engine and template matching.
   - `internal/logicalunit`: port logical unit pool logic.

3. **Replace Servlet/JSP layer with Go net/http**
   - Map Java `doGet` / `doPost` to Go handlers.
   - Replace session storage with a Go session layer.
   - Convert JSPs to Go `html/template`.

4. **Configuration**
   - Parse `WEB-INF/h3270-config.xml` in Go.
   - Provide configuration lookups matching `H3270Configuration`.

5. **Portlet**
   - Port last or drop, depending on deployment needs.

## 3) Proposed Go module layout
```
/cmd/h3270-web        # main server
/internal/host        # s3270 subprocess + screen parsing
/internal/render      # renderers + template engine
/internal/session     # session state
/internal/config      # XML config
/web/templates        # converted JSPs
/static               # css/js
```

## 4) Key risks
- Maintaining exact `s3270` command/response semantics.
- Session state differences between servlet container and Go.
- JSP → Go template parity.

## 5) Validation checkpoints
- Unit tests for screen parsing with captured dumps.
- Snapshot tests for rendered HTML.
- End-to-end test for connect / disconnect / submit / refresh paths.
