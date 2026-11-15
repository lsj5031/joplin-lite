// Offline evidence generator. Never starts Joplin or opens a profile.
// Usage: node qa/official-envelope.mjs /absolute/path/main-html.bundle.js output.json
import fs from 'node:fs';
import vm from 'node:vm';
import crypto from 'node:crypto';
import { DatabaseSync } from 'node:sqlite';

const [bundle, output] = process.argv.slice(2);
const source = fs.readFileSync(bundle, 'utf8');
const sourceSHA256 = crypto.createHash('sha256').update(source).digest('hex');
if (sourceSHA256 !== 'b2143adff1589d07bd5c1dd83606ecbe571b61e5e6e27fa94357727e83675548') {
  throw new Error('Expected the recorded official 3.7.22 bundle; refusing unverified code');
}
const between = (a, b) => {
  const start = source.indexOf(a), end = source.indexOf(b, start);
  if (start < 0 || end < 0) throw new Error(`Missing extraction boundary: ${a}`);
  return source.slice(start, end);
};
const db = new DatabaseSync(':memory:');
const tables = { 1: 'notes', 2: 'folders', 4: 'resources', 5: 'tags', 6: 'note_tags' };
const wanted = new Set(Object.values(tables));
wanted.add('revisions'); // Validate-only passthrough in the repair operator.
let revisionBaseColumns;
const run = q => {
  const sql = typeof q === 'string' ? q : q.sql;
  if (sql.startsWith('DROP TABLE ') && wanted.has(sql.slice(11))) {
    db.exec(sql);
    return;
  }
  const match = sql.match(/^(?:CREATE TABLE|ALTER TABLE)\s+[`"]?(\w+)/i);
  if (match && wanted.has(match[1])) {
    db.exec(sql);
    if (match[1] === 'revisions') {
      const columns = db.prepare('pragma table_info(revisions)').all();
      if (columns.length === 12 && !columns.some(c => c.name === 'is_locked')) {
        revisionBaseColumns = columns;
      }
    }
  }
};
// Execute shipped migrations for the five repaired tables and revision history.
const context = vm.createContext({
  console, Date, run,
  o: x => x,
  S: (f, m) => () => { if (!m) { m = { exports: {} }; f(m.exports, m); } return m.exports; },
});
const migrations = between('var sKt=S(', 'var tti=S(');
const initial = between('bMs=`', '`,tfe=class').slice(5);
const upgrade = between('async upgradeDatabase(t)', 'async ftsEnabled()');
await vm.runInContext(`
${migrations}
const eti=Zei(), lC=cKt(), att=sKt(), bMs=\`${initial}\`;
const gMs={default:{FETCH_STATUS_DONE:2}};
const database={
  ${upgrade},
  logger:()=>({info(){},warn(){}}),
  wrapQueries:q=>q,
  alterColumnQueries:(table,fields)=>{
    if (!${JSON.stringify([...wanted])}.includes(table)) return [];
    return ['DROP TABLE '+table, 'CREATE TABLE '+table+' ('+
      Object.entries(fields).map(([k,v])=>'"'+k+'" '+v).join(',')+')'];
  },
  transactionExecBatch:async queries=>{for(const q of queries) run(q)},
};
database.upgradeDatabase(0);
`, context);
// Field order is SQLite's actual migrated column order, as BaseModel.fieldNames uses it.
const fields = Object.fromEntries(Object.entries(tables).map(([type, table]) =>
  [type, db.prepare(`pragma table_info(${table})`).all()]));
const revisionColumns = db.prepare('pragma table_info(revisions)').all();
fields[13] = revisionColumns;
const methods = [
  between('static serialize_format(t,n)', 'static unserialize_format('),
  between('static unserialize_format(t,n,r)', 'static async serialize('),
  between('static async serialize(t,n=null)', 'static encryptionService('),
  between('static async serializeForSync(t)', 'static async decrypt('),
  between('static async unserialize(t)', 'static async encryptedItemsStats('),
].join('\n');
// Only external boundaries are adapted: field registry, date library, scalar conversion.
context.fields = fields;
const Official = vm.runInContext(`
const rL={default:{TYPE_NOTE:1}},nWe={default:Error};
const xxa={getEncryptionEnabled:()=>true},_xa={default:()=>true};
const Oxa={default:{formatValue:(type,value)=>type==='TEXT'?String(value):Number(value)}};
const Mmr=Object.assign(value=>({format:()=>String(Date.parse(value))}),{
  unix:value=>({utc:()=>({format:()=>new Date(value*1000).toISOString().slice(0,-1)})})
});
class Official {
  static itemClass(item) {
    const type=typeof item==='object'?item.type_:item;
    return {
      fieldNames:()=>fields[type].map(f=>f.name),
      fieldType:name=>fields[type].find(f=>f.name===name)?.type,
      removeUnknownFields:item=>Object.fromEntries(Object.entries(item).filter(([k])=>k==='type_'||fields[type].some(f=>f.name===k))),
      serialize:(...args)=>Official.serialize(...args),
      encryptionSupported:()=>true,
    };
  }
  static filter(item){return item}
  static encryptionService(){return {encryptString:async()=> 'synthetic-ciphertext'}}
  ${methods}
}
Official;
`, context);
const items = {};
for (const [type, columns] of Object.entries(fields)) {
  if (type === '13') continue;
  const item = {
    id: 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
    updated_time: 1772000000000, type_: Number(type),
    title: 'private title', body: 'private body',
    created_time: 1771000000000, user_created_time: 1771000000000,
    user_updated_time: 1772000000000, encryption_applied: 0,
  };
  const text = await Official.serializeForSync(item);
  const parsed = await Official.unserialize(text);
  // Synchronizer's actual fallback expressions.
  parsed.user_updated_time ||= parsed.updated_time;
  parsed.user_created_time ||= parsed.created_time;
  const broken = await Official.unserialize(text.split('\n').filter(line =>
    !line.endsWith(': ')).join('\n'));
  broken.user_created_time ||= broken.created_time;
  broken.user_updated_time ||= broken.updated_time;
  const insert = item => db.prepare(`insert into ${tables[type]} (${Object.keys(item)
    .filter(k => k !== 'type_').map(k => `"${k}"`).join(',')}) values (${Object.keys(item)
    .filter(k => k !== 'type_').map(() => '?').join(',')})`).run(...Object.entries(item)
    .filter(([k]) => k !== 'type_').map(([, v]) => v === undefined ? null : v));
  let malformedError;
  insert(parsed);
  // Existing-item update, as in Synchronizer's updateLocal path. New-item
  // saveQuery can assign timestamps; that is not the failing path.
  const update = item => db.prepare(`update ${tables[type]} set ${Object.keys(item)
    .filter(k => k !== 'type_').map(k => `"${k}" = ?`).join(',')}`).run(...Object.entries(item)
    .filter(([k]) => k !== 'type_').map(([, v]) => v === undefined ? null : v));
  try { update(broken); } catch (e) { malformedError = e.message; }
  if (!malformedError?.includes('NOT NULL constraint failed')) throw new Error('Missing negative control');
  update(parsed);
  items[type] = { columns, text, parsed, malformedError };
}
const revisionText = await Official.serializeForSync({
  id: 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
  updated_time: 1772000000000, type_: 13,
});
const revisionEnvelope = {
  baseColumns: revisionBaseColumns,
  columns: revisionColumns,
  text: revisionText,
  parsed: await Official.unserialize(revisionText),
};
fs.writeFileSync(output, JSON.stringify({
  sourceSHA256, items, revisionEnvelope,
}, null, 2) + '\n');
