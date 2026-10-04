import { parseNode, parseStatement, type Node, type Statement } from '@flanksource/uir';

export const node: Node = parseNode({ node_kind: 'ref', method: 'Run' });
export const statement: Statement = parseStatement({ statement_type: 'dispatch_call', candidates: [node] });
