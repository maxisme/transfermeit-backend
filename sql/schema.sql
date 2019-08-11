create table user
(
    id int auto_increment
        primary key,
    code varchar(255) collate utf8_bin null,
    code_end_dttm datetime not null,
    wanted_mins int default 10 null,
    is_connected tinyint(1) default 0 not null,
    UUID varchar(255) default '' not null,
    UUID_key varchar(255) default '' not null,
    public_key varchar(1000) null,
    registered_dttm datetime not null,
    constraint devUUID
        unique (UUID),
    constraint id
        unique (code)
);

create table credit
(
    id int auto_increment
        primary key,
    activation_code varchar(255) null,
    created_dttm timestamp null,
    credit float not null,
    email varchar(767) default '' not null,
    activation_dttm timestamp null,
    UUID varchar(500) null,
    perm_user_code varchar(255) null,
    custom_user_code varchar(255) null,
    constraint email
        unique (email),
    constraint perm_user
        unique (perm_user_code),
    constraint pro_custom_user_code_uindex
        unique (custom_user_code),
    constraint pro_pk
        unique (perm_user_code, custom_user_code),
    constraint credit_ibfk_1
        foreign key (UUID) references user (UUID)
);

create index UUID
    on credit (UUID);

create table upload
(
    id int auto_increment
        primary key,
    expiry_dttm timestamp default CURRENT_TIMESTAMP null,
    file_path varchar(200) null,
    size int(255) unsigned default 0 not null,
    from_UUID varchar(255) default '' not null,
    to_UUID varchar(255) null,
    updated_dttm timestamp null,
    finished_dttm timestamp null,
    failed tinyint(1) default 0 not null,
    file_hash varchar(128) null,
    password text null,
    constraint path
        unique (file_path),
    constraint upload_ibfk_1
        foreign key (from_UUID) references user (UUID),
    constraint upload_ibfk_2
        foreign key (to_UUID) references user (UUID)
);

create index fromID
    on upload (from_UUID);

create index toID
    on upload (to_UUID);

