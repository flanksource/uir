import"./index-BYz-j3Hr.js";var e={accentColor:`light-dark(black, white)`,basePath:`/uir`,codeHighlight:{langAlias:{sol:`solidity`,js:`javascript`,cjs:`javascript`,mjs:`javascript`,md:`markdown`,jade:`pug`,ts:`typescript`,cts:`typescript`,mts:`typescript`,coffeescript:`coffee`,regex:`regexp`,"c++":`cpp`,gql:`graphql`,yml:`yaml`,hbs:`handlebars`,bash:`shellscript`,sh:`shellscript`,shell:`shellscript`,zsh:`shellscript`,py:`python`,jl:`julia`,styl:`stylus`,lit:`ts-tags`,dockerfile:`docker`,protobuf:`proto`,rs:`rust`},langs:[`ansi`,`bash`,`html`,`js`,`json`,`jsx`,`markdown`,`md`,`mdx`,`plaintext`,`rust`,`sol`,`solidity`,`ts`,`tsx`,`zsh`,`typescript`,`go`,`sql`,`javascript`,`text`,`sh`,`yaml`],themes:{light:`github-light`,dark:`github-dark-dimmed`}},colorScheme:`light dark`,description:`A universal intermediate representation for indexing, querying, and comparing code.`,feedback:!1,jsonLd:!0,renderStrategy:`full-static`,search:{query:{boostDocument:`_vocs-fn_(_id, _term, storedFields) => {
                    const priority = storedFields?.['searchPriority'] ?? 1;
                    const href = storedFields?.['href'];
                    const isDocsPath = href?.startsWith('/docs/');
                    // Treat /docs/ as root for depth calculation (subtract 1)
                    const segments = href ? href.split('/').filter(Boolean).length : 1;
                    const depth = isDocsPath ? Math.max(segments - 1, 1) : segments;
                    const depthBoost = 1 / Math.max(depth, 1);
                    const docsBoost = isDocsPath ? 1.5 : 1;
                    return priority * depthBoost * docsBoost;
                }`,combineWith:`AND`,fuzzy:.2,prefix:!0,boost:{title:4,subtitle:3,text:2,category:1,titles:1}},boostDocument:`_vocs-fn_(_id, _term, storedFields) => {
                    const priority = storedFields?.['searchPriority'] ?? 1;
                    const href = storedFields?.['href'];
                    const isDocsPath = href?.startsWith('/docs/');
                    // Treat /docs/ as root for depth calculation (subtract 1)
                    const segments = href ? href.split('/').filter(Boolean).length : 1;
                    const depth = isDocsPath ? Math.max(segments - 1, 1) : segments;
                    const depthBoost = 1 / Math.max(depth, 1);
                    const docsBoost = isDocsPath ? 1.5 : 1;
                    return priority * depthBoost * docsBoost;
                }`,combineWith:`AND`,fuzzy:.2,prefix:!0,boost:{title:4,subtitle:3,text:2,category:1,titles:1}},sidebar:[{text:`Introduction`,link:`/`},{text:`Getting started`,link:`/getting-started`},{text:`Index and publish`,items:[{text:`Module indexing`,link:`/module-indexing`},{text:`Go indexing`,link:`/indexing`},{text:`Git-backed publication`,link:`/git-backed-publication`},{text:`Historical imports`,link:`/historical-import`}]},{text:`Explore and check`,items:[{text:`Compact queries`,link:`/query`},{text:`SQL examples`,link:`/sql-examples`},{text:`SQL lint rules`,link:`/lint`},{text:`Call graphs`,link:`/graph`},{text:`Symbol diffs`,link:`/diff`},{text:`Test runs and flakiness`,link:`/tests`},{text:`Browser and API`,link:`/serve`}]},{text:`Reference`,items:[{text:`CLI configuration`,link:`/cli`},{text:`SQL language`,link:`/sql-query`},{text:`UIR model`,link:`/model`},{text:`Symbols and references`,link:`/symbols`}]},{text:`Architecture and design`,items:[{text:`Architecture`,link:`/architecture`},{text:`Storage`,link:`/storage`},{text:`Storage ERD`,link:`/storage-erd`},{text:`Symbol index storage`,link:`/symbol-index-storage`},{text:`Compact symbol handles`,link:`/symbol-compact-ids`},{text:`Benchmarks`,link:`/benchmarks`}]}],title:`UIR`,titleTemplate:`%s – UIR`,topNav:[{text:`Guides`,link:`/getting-started`},{text:`Model`,link:`/model`},{text:`GitHub`,link:`https://github.com/flanksource/uir`,external:!0}]};export{e as t};