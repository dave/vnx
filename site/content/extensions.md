---
title: Extensions
nav: Extensions
description: How to extend VNX with your own data. Data records, reverse-DNS type IDs, application keys, attachment folders, and what readers do with types they do not know.
---

# Extensions

VNX defines geometry and structure. It defines **no** data types. Every
application adds its own information through extensions, and every reader
keeps extensions it does not understand. This page explains how.

There are three ways to extend a document:

| Mechanism | Use it for | Where it goes |
|---|---|---|
| **Data record** | information about one item or arc: a rating, a tag, a note, a surface, a photo | the `data` array of that item or arc |
| **Application key** | a new kind of thing that is not about one item: a set of routing rules, a type catalogue | a top-level key with a full stop in its name |
| **Attachment** | files that records or keys refer to: photos, audio, documents | a folder in a `.vnz` archive |

## Data records

Every item (folder, network, highlight, plan, track, waypoint, point, area) and
every arc can have a `data` array. Each entry is a record: a JSON object with a
`@type` and any other fields (§5).

```json
{
  "name": "Spring",
  "at": [-3.6521, 57.1, 940],
  "data": [
    { "@type": "com.example.water", "value": "reliable", "treated": false },
    { "@type": "com.example.tag", "value": "resupply" },
    { "@type": "com.example.tag", "value": "day-2" }
  ]
}
```

### Rules for records

1. **`data` is a list, not a map.** A record type can appear more than once. Two
   tags are two records. VNX does not say what a reader does with several
   records of one type. Your type defines that.
2. **The body is yours.** Apart from `@type`, a reader does not look inside a
   record. It does not check field types, ranges or names. A field may even be
   called `type`.
3. **Unknown types are kept.** A reader that does not know a type keeps the
   record, does not report it, and writes it back unchanged after an edit. This
   is true on every host, including a host where the reader did not expect the
   type.
4. **Meaning is type plus host.** One type can mean one thing on a highlight and
   another on an area. Say in your documentation which hosts your type is for.
5. **One main value is called `value`.** A type with a single main field names
   it `value`. A type with several fields uses its own names, and can also have
   `value`.

Why `@type`: the `@` marks the key as structure, not data. VNX borrows the
spelling from JSON-LD, but a VNX document is not JSON-LD and has no `@context`.

### Where to put a record

- On a **highlight**, a record applies along the highlight's walk: "this
  stretch is a scramble". This is the main use of highlights. A highlight has
  no notes or style, so its records are what it is.
- On an **arc**, a record applies to that piece of trail itself, whatever walks
  use it: "this arc was routed by an engine".
- On a **point, waypoint or area**, a record describes that place.
- On a **network, plan, track or folder**, a record describes the whole item.

## Type IDs

A record's `@type` is a **reverse-DNS identifier**: a domain you control,
written backwards, then a name for the type.

| You own | Your type IDs could be |
|---|---|
| `example.com` | `com.example.tag`, `com.example.water`, `com.example.photo` |
| `trails.example.org` | `org.example.trails.grade` |
| `example.app` | `app.example.surface` |

- **Use a domain you own.** The owner of the domain decides what names under
  it mean, so two organisations cannot choose the same ID by accident.
- **IDs are compared exactly, letter case included.** `com.example.Tag` and
  `com.example.tag` are two different types. Use lower case.
- **Keep an ID stable.** A reader matches records by ID only. If you rename a
  type, files with the old ID become records of an unknown type.
- **Version inside the record if you need to.** VNX does not version record
  types. If a type's fields will change, add a field such as `"v": 2`, or use a
  new ID.
- **Do not use a domain you do not own,** and do not use VNX's own words
  without a domain (`tag`, `note`). A reader does not validate the form, but
  the form is what prevents clashes.

## Application keys

A top-level key whose name contains a full stop is an **application key**. It
adds a kind of content that VNX does not define (§3.9).

```json
{
  "vnx": "0.10.0",
  "networks": [],
  "com.example.routes": {
    "profiles": [{ "id": "dry", "avoid": ["com.example.bog"] }]
  }
}
```

- Name it with reverse DNS, the same as a record type.
- The value can be any JSON value. It is opaque: a reader applies no VNX rule
  inside it.
- A reader keeps the key and its value unchanged through a read and a write. A
  `null` stays `null`, and an empty array stays empty.
- A reader does not report an application key.
- Write each application key once only.
- Application keys exist at the top level only. On any other object, a key with
  a full stop is an unknown key, and the reader drops it and reports it.

Choose a record when the information is about one item. Choose an application
key when it is a new kind of thing, such as a catalogue of your data types or a
set of rules that many items use.

## Attachments in a VNZ archive

A `.vnz` archive can hold any files beside `doc.vnx` (§7). Put the files of each
data type in a folder named exactly after its type ID (§7.2):

```
doc.vnx
com.example.photo/3f2a9c.jpg
com.example.photo/77b01e.jpg
org.example.audio/ridge-notes.m4a
```

Then the record names its file by the full entry name:

```json
{ "@type": "com.example.photo", "files": ["com.example.photo/3f2a9c.jpg"], "caption": "Summit cairn" }
```

- Two extensions in one archive then cannot have the same file name, because
  each uses its own folder.
- Files for an application key go in a folder named after the key.
- The folder name is a recommendation for writers. A reader does not interpret
  it. A reader returns every file in the archive as an attachment, and does not
  report a file that no record names.
- Do not decide what a file is from its extension. Your record says what it is.

## What a reader does with extensions

| It finds | It does |
|---|---|
| a record of a type it knows | uses it |
| a record of a type it does not know | keeps it, writes it back unchanged, reports nothing |
| a record with no usable `@type` | drops that record and reports a diagnostic |
| an application key it knows | uses it |
| an application key it does not know | keeps it, writes it back unchanged, reports nothing |
| a dotted key on any other object | drops the key and reports a diagnostic |
| a file in a `.vnz` that no record names | returns it as an attachment, reports nothing |

An application can be stricter about its **own** namespace. For example, it
can refuse a record under its own domain that its version does not know. That
is the application's rule, not a VNX rule.

## Document your extension

If other people will read or write your extension, publish:

- each type ID, its fields, their JSON types and meaning;
- which hosts each type is for (highlight, arc, waypoint, and so on);
- whether a type can repeat on one item, and what several records mean;
- each application key and the shape of its value;
- the folder and file names your types use in a `.vnz`;
- a JSON Schema for your records, if you can.

## Example VNX extensions

These are extensions that applications have published. They are examples of
the mechanism on this page. They are not part of VNX, and a VNX reader does not
need them.

- [Viatrail extensions](https://viatrail.app/vnx): the data types, application
  keys and photo attachments of the Viatrail app, with a JSON Schema.
