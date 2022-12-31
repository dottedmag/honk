//
// Copyright (c) 2019 Ted Unangst <tedu@tedunangst.com>
//
// Permission to use, copy, modify, and distribute this software for any
// purpose with or without fee is hereby granted, provided that the above
// copyright notice and this permission notice appear in all copies.
//
// THE SOFTWARE IS PROVIDED "AS IS" AND THE AUTHOR DISCLAIMS ALL WARRANTIES
// WITH REGARD TO THIS SOFTWARE INCLUDING ALL IMPLIED WARRANTIES OF
// MERCHANTABILITY AND FITNESS. IN NO EVENT SHALL THE AUTHOR BE LIABLE FOR
// ANY SPECIAL, DIRECT, INDIRECT, OR CONSEQUENTIAL DAMAGES OR ANY DAMAGES
// WHATSOEVER RESULTING FROM LOSS OF USE, DATA OR PROFITS, WHETHER IN AN
// ACTION OF CONTRACT, NEGLIGENCE OR OTHER TORTIOUS ACTION, ARISING OUT OF
// OR IN CONNECTION WITH THE USE OR PERFORMANCE OF THIS SOFTWARE.

package main

import (
	"context"
	"database/sql"

	"github.com/dottedmag/sqv"
)

const (
	honkAppID     = 0x7677
	honkBlobAppID = 0x7678
)

var schema = []string{
	`
create table honks (
  honkid integer primary key,
  userid integer,
  what text,
  honker text,
  xid text,
  rid text,
  dt text,
  url text,
  audience text,
  noise text,
  thread text,
  whofore integer,
  format text,
  precis text,
  oonker text,
  flags integer
);
create index idx_honksxid on honks(xid);
create index idx_honksthread on honks(thread);
create index idx_honkshonker on honks(honker);
create index idx_honksoonker on honks(oonker);

create table chatMessages (
  chatMessageId integer primary key,
  userid integer,
  xid text,
  who txt,
  target text,
  dt text,
  noise text,
  format text
);

create table attachments (
  honkid integer,
  chatMessageId integer,
  fileid integer
);
create index idx_attachmentsHonk on attachments(honkid);
create index idx_attachmentsChatMessageId on attachments(chatMessageId);

create table filemeta (
  fileid integer primary key,
  xid text,
  name text,
  description text,
  url text,
  media text,
  local integer
);
create index idx_filesxid on filemeta(xid);
create index idx_filesurl on filemeta(url);

create table honkers (
  honkerid integer primary key,
  userid integer,
  name text,
  xid text,
  flavor text,
  combos text,
  owner text,
  meta text,
  folxid text
);
create index idx_honkerxid on honkers(xid);

create table xonkers (
  xonkerid integer primary key,
  name text,
  info text,
  flavor text,
  dt text
);
create index idx_xonkername on xonkers(name);

create table actions (
  actionID integer primary key,
  userid integer,
  name text,
  action text
);
create index idx_actionsName on actions(name);

create table resubmissions(
  resubmissionid integer primary key,
  dt text,
  tries integer,
  userid integer,
  rcpt text,
  msg blob
);

create table onts (
  ontology text,
  honkid integer
);
create index idx_ontology on onts(ontology);
create index idx_onthonkid on onts(honkid);

create table honkmeta (
  honkid integer,
  genus text,
  json text
);
create index idx_honkmetaid on honkmeta(honkid);

create table hfcs (
  hfcsid integer primary key,
  userid integer,
  json text
);
create index idx_hfcsuser on hfcs(userid);

create table tracks (
  xid text,
  fetches text
);
create index idx_trackhonkid on tracks(xid);

create table config (
  key text,
  value text
);

create table users (
  userid integer primary key,
  username text,
  hash text,
  displayname text,
  about text,
  pubkey text,
  seckey text,
  options text
);
CREATE index idxusers_username on users(username);

create table auth (
  authid integer primary key,
  userid integer,
  hash text,
  expiry text
);
CREATE index idxauth_userid on auth(userid);
CREATE index idxauth_hash on auth(hash);
`,
}

var blobSchema = []string{
	`
create table filedata (
  xid text,
  media text,
  hash text,
  content blob
);
create index idx_filexid on filedata(xid);
create index idx_filehash on filedata(hash);
`,
}

func upgradeDB(ctx context.Context, db *sql.DB) error {
	return sqv.Apply(ctx, db, honkAppID, schema)
}

func upgradeBlobDB(ctx context.Context, blobdb *sql.DB) error {
	return sqv.Apply(ctx, blobdb, honkBlobAppID, blobSchema)
}
