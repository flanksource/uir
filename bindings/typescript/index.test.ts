import { describe, expect, it } from 'vitest';
import { parseNode, parseStatement, parseUIR } from './index.js';

describe('generated wire model validation', () => {
  it('preserves empty, null and false metadata without adding defaults', () => {
    const node = {
      node_kind: 'method',
      method: 'Run',
      properties: { empty: '', list: [], nullable: null, enabled: false },
    };
    expect(parseNode(node)).toBe(node);
    expect(JSON.parse(JSON.stringify(parseNode(node)))).toEqual(node);
  });

  it('accepts both document wire forms', () => {
    const nodes = [{ node_kind: 'ref', method: 'Run' }];
    expect(parseUIR(nodes)).toBe(nodes);
    expect(parseUIR({})).toEqual({});
  });

  it('accepts recursive types whose optional constructor is absent', () => {
    const node = { node_kind: 'class', type: 'Outer', types: [{ type: 'Inner' }] };
    expect(parseNode(node)).toBe(node);
  });

  it.each([
    {},
    { node_kind: 'unknown' },
    { node_kind: 'ref', method: 'Run', unexpected: true },
    { node_kind: 'field' },
  ])('rejects malformed polymorphic nodes: %j', (value) => {
    expect(() => parseNode(value)).toThrow();
  });

  it.each([
    {},
    { statement_type: 'unknown' },
    { statement_type: 'call', statement_refinement: 'unknown' },
    { statement_type: 'call', statement_refinement: 'call\n' },
    { statement_type: 'call', statement_refinement: 'call:api' },
    { statement_type: 'call', statement_refinement: 'call:api:tenant' },
    { statement_type: 'dispatch_call', statement_refinement: 'call:package' },
  ])('rejects invalid discriminators and refinements: %j', (value) => {
    expect(() => parseStatement(value)).toThrow();
  });

  it.each([
    { statement_type: 'call', statement_refinement: 'call:package' },
    { statement_type: 'call', statement_refinement: 'call:\n' },
    { statement_type: 'call', statement_refinement: 'call:api\n' },
    { statement_type: 'control:block', statement_refinement: 'doc' },
    { statement_type: 'dispatch_call', candidates: [{ node_kind: 'ref', method: 'Run' }] },
  ])('retains supported refinements and dispatch candidates: %j', (value) => {
    expect(parseStatement(value)).toBe(value);
  });
});
