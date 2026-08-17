# Research Questions

## Context

Focus on the Beanfun client package (`internal/beanfun/`), specifically the OTP fetch chain
and its supporting pieces: request construction, response scraping, error classification,
and the DES helper. Also cover how the launcher service and the frontend consume errors from
that package, and which code paths contact each Gamania host.

## Questions

1. How does the OTP fetch run end to end — which host does each step contact, and what does
   each step scrape from or send to the server?
2. How is the final step's query string assembled, and what encoding is applied to each
   value?
3. How does the package classify server errors, what is embedded in each error's message,
   and how does the launcher service and the frontend react to each class?
4. What does the page fetched by the OTP flow's first step actually contain, and which of
   its values are consumed today?
5. Which code paths contact `tw.beanfun.com`, and at what frequency?
6. What does the existing DES helper accept and produce, and where else could it be reused?
