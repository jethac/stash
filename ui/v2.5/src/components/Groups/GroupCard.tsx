import React, { useMemo } from "react";
import { Button, ButtonGroup } from "react-bootstrap";
import { useLocation } from "react-router-dom";
import * as GQL from "src/core/generated-graphql";
import { PatchComponent } from "src/patch";
import { GridCard } from "../Shared/GridCard/GridCard";
import { HoverPopover } from "../Shared/HoverPopover";
import { Icon } from "../Shared/Icon";
import { SceneLink, TagLink } from "../Shared/TagLink";
import { TruncatedText } from "../Shared/TruncatedText";
import { FormattedMessage } from "react-intl";
import { RatingBanner } from "../Shared/RatingBanner";
import {
  faFilm,
  faPlayCircle,
  faTag,
  faUser,
} from "@fortawesome/free-solid-svg-icons";
import { RelatedGroupPopoverButton } from "./RelatedGroupPopover";
import { OCounterButton } from "../Shared/CountButton";
import TextUtils from "src/utils/text";
import { getGroupDisplayTitle, NATIVE_TITLE_LANGUAGE } from "src/core/groups";

const Description: React.FC<{
  sceneNumber?: number;
  description?: string;
}> = ({ sceneNumber, description }) => {
  if (!sceneNumber && !description) return null;

  return (
    <>
      <hr />
      {sceneNumber !== undefined && (
        <span className="group-scene-number">
          <FormattedMessage id="scene" /> #{sceneNumber}
        </span>
      )}
      {description !== undefined && (
        <span className="group-containing-group-description">
          {description}
        </span>
      )}
    </>
  );
};

interface IProps {
  group: GQL.ListGroupDataFragment;
  cardWidth?: number;
  sceneNumber?: number;
  selecting?: boolean;
  selected?: boolean;
  zoomIndex?: number;
  onSelectedChanged?: (selected: boolean, shiftKey: boolean) => void;
  fromGroupId?: string;
  onMove?: (srcIds: string[], targetId: string, after: boolean) => void;
  titleLanguage?: string;
}

export const GroupCard: React.FC<IProps> = PatchComponent(
  "GroupCard",
  ({
    group,
    sceneNumber,
    cardWidth,
    selecting,
    selected,
    zoomIndex,
    onSelectedChanged,
    fromGroupId,
    onMove,
    titleLanguage = NATIVE_TITLE_LANGUAGE,
  }) => {
    const location = useLocation();
    const displayTitle = getGroupDisplayTitle(group, titleLanguage);
    const basePath = location.pathname.startsWith("/movies")
      ? "/movies"
      : "/groups";
    const metadata = [
      group.date?.slice(0, 4),
      group.studio?.name,
      group.duration ? TextUtils.secondsToTimestamp(group.duration) : undefined,
    ].filter((value): value is string => !!value);

    const groupDescription = useMemo(() => {
      if (!fromGroupId) {
        return undefined;
      }

      const containingGroup = group.containing_groups.find(
        (cg) => cg.group.id === fromGroupId
      );

      return containingGroup?.description ?? undefined;
    }, [fromGroupId, group.containing_groups]);

    function maybeRenderScenesPopoverButton() {
      if (group.scenes.length === 0) return;

      const popoverContent = group.scenes.map((scene) => (
        <SceneLink key={scene.id} scene={scene} />
      ));

      return (
        <HoverPopover
          className="scene-count"
          placement="bottom"
          content={popoverContent}
        >
          <Button className="minimal">
            <Icon icon={faPlayCircle} />
            <span>{group.scenes.length}</span>
          </Button>
        </HoverPopover>
      );
    }

    function maybeRenderTagPopoverButton() {
      if (group.tags.length <= 0) return;

      const popoverContent = group.tags.map((tag) => (
        <TagLink key={tag.id} linkType="group" tag={tag} />
      ));

      return (
        <HoverPopover placement="bottom" content={popoverContent}>
          <Button className="minimal tag-count">
            <Icon icon={faTag} />
            <span>{group.tags.length}</span>
          </Button>
        </HoverPopover>
      );
    }

    function maybeRenderOCounter() {
      if (!group.o_counter) return;

      return <OCounterButton value={group.o_counter} />;
    }

    function maybeRenderPopoverButtonGroup() {
      if (
        sceneNumber ||
        groupDescription ||
        group.scenes.length > 0 ||
        group.tags.length > 0 ||
        group.containing_groups.length > 0 ||
        group.sub_group_count > 0
      ) {
        return (
          <>
            <Description
              sceneNumber={sceneNumber}
              description={groupDescription}
            />
            <hr />
            <ButtonGroup className="card-popovers">
              {maybeRenderScenesPopoverButton()}
              {maybeRenderTagPopoverButton()}
              {(group.sub_group_count > 0 ||
                group.containing_groups.length > 0) && (
                <RelatedGroupPopoverButton group={group} />
              )}
              {maybeRenderOCounter()}
            </ButtonGroup>
          </>
        );
      }
    }

    function renderPoster() {
      if (group.front_image_path) {
        return (
          <img
            loading="lazy"
            className="group-card-image"
            alt={displayTitle}
            src={group.front_image_path}
          />
        );
      }

      return (
        <div className="group-card__missing-poster">
          <Icon icon={faFilm} />
          <span>
            <FormattedMessage id="missing_poster" defaultMessage="No poster" />
          </span>
        </div>
      );
    }

    function maybeRenderMetadata() {
      if (metadata.length === 0) return null;

      return (
        <div className="group-card__meta">
          {metadata.map((value) => (
            <span key={value} className="group-card__meta-item">
              {value}
            </span>
          ))}
        </div>
      );
    }

    return (
      <GridCard
        className={`group-card zoom-${zoomIndex}`}
        objectId={group.id}
        onMove={onMove}
        url={`${basePath}/${group.id}`}
        width={cardWidth}
        title={displayTitle}
        linkClassName="group-card-header"
        image={
          <>
            {renderPoster()}
            <RatingBanner rating={group.rating100} />
          </>
        }
        details={
          <div className="group-card__details">
            {maybeRenderMetadata()}
            <div className="group-card__counts">
              <span className="group-card__count">
                <Icon icon={faPlayCircle} />
                {group.scene_count}
              </span>
              <span className="group-card__count">
                <Icon icon={faUser} />
                {group.performer_count}
              </span>
            </div>
            <TruncatedText
              className="group-card__description"
              text={group.synopsis}
              lineCount={2}
            />
          </div>
        }
        selected={selected}
        selecting={selecting}
        onSelectedChanged={onSelectedChanged}
        popovers={maybeRenderPopoverButtonGroup()}
      />
    );
  }
);
