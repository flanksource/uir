import { Ajv2020, type ValidateFunction } from 'ajv/dist/2020.js';
import { fullFormats } from 'ajv-formats/dist/formats.js';
import schema from './uir.schema.json';
import type { Node, Statement, UIR } from './model.js';
export type * from './model.js';

const ajv = new Ajv2020({ formats: fullFormats, ownProperties: true });
ajv.addSchema(schema, 'uir');
const nodeValidator = ajv.compile<Node>({ $ref: 'uir#/$defs/Node' });
const statementValidator = ajv.compile<Statement>({ $ref: 'uir#/$defs/Statement' });
const documentValidator = ajv.compile<UIR>({ $ref: 'uir' });

function parse<T>(value: unknown, validator: ValidateFunction<T>): T {
  if (!validator(value)) {
    throw new TypeError(`Invalid UIR: ${ajv.errorsText(validator.errors)}`);
  }
  return value;
}

export function parseNode(value: unknown): Node {
  return parse(value, nodeValidator);
}

export function parseStatement(value: unknown): Statement {
  return parse(value, statementValidator);
}

export function parseUIR(value: unknown): UIR {
  return parse(value, documentValidator);
}
