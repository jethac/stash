CREATE TABLE `localized_titles` (
  `id` integer not null primary key autoincrement,
  `object_type` varchar(32) not null,
  `object_id` integer not null,
  `language_code` varchar(32) not null,
  `title` text not null,
  `source` varchar(255),
  `created_at` datetime not null,
  `updated_at` datetime not null,
  UNIQUE (`object_type`, `object_id`, `language_code`)
);

CREATE INDEX `index_localized_titles_on_object` ON `localized_titles` (`object_type`, `object_id`);
CREATE INDEX `index_localized_titles_on_language_title` ON `localized_titles` (`language_code`, `title`);
