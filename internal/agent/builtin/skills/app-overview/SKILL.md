---
name: app-overview
description: Summarise a whole application for a stakeholder — what it is, its main areas, the operations it supports, and what is still not understood.
invocation: [explain-app, summarize-app, document-app]
tools: [business_app_overview, chaos_list_screens, chaos_annotate_screen, chaos_insights, business_save_function]
instructions: [untrusted-host-data]
---

# Whole-application overview

Answer "what is this application?" rather than "what is on this screen?".

## When to use

The user asks you to understand, explain, map, document or summarise the
application as a whole — not a single screen.

## Steps

1. Call `business_app_overview` **first**. One call returns coverage stats,
   every discovered screen with its business purpose, key fields and
   navigation, the catalogued business functions, and an explicit `gaps`
   section naming what is not yet understood.

   If it reports no screens discovered, run the `chaos-monkey` skill and come
   back.

2. Give the user a business summary: what the application is, its main areas,
   and the operations it supports. A short bullet list or a small table — the
   chat panel is narrow, so prose paragraphs read badly there.

3. Close the gaps the overview reports:

   - Screens with no annotation — infer their purpose from
     `chaos_list_screens` previews and call `chaos_annotate_screen`.
   - Screens with input fields but no known working values — propose hints,
     or drive them manually to learn values. `chaos_insights` ranks which are
     worth the effort.
   - Business functions with no examples — fill them in via
     `business_save_function`.

4. Once the gaps are meaningfully closed, offer to catalogue any missing
   business functions and to export workflows for the important ones.

## Anti-patterns

- Reconstructing the overview screen by screen when
  `business_app_overview` returns it in one call.
- Presenting coverage as understanding. Thirty annotated screens and no
  catalogued function means nobody can *do* anything with the map.
- Quietly omitting the gaps. The list of what is not understood is the most
  useful part of the summary for whoever has to plan the next session.
