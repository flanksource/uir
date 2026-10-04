import { parseNode, parseStatement } from '../../../bindings/typescript/dist/index.js';

let input = '';
for await (const chunk of process.stdin) input += chunk;
const cases = JSON.parse(input);
const output = cases.map((item) => {
  let value;
  switch (item.hierarchy) {
    case 'Node': value = parseNode(item.value); break;
    case 'Statement': value = parseStatement(item.value); break;
    default: throw new TypeError(`Unknown hierarchy: ${item.hierarchy}`);
  }
  return { ...item, value };
});
process.stdout.write(JSON.stringify(output));
