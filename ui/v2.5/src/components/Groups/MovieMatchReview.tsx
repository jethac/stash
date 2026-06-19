import React, { useMemo, useState } from "react";
import { Alert, Button, Form, Table } from "react-bootstrap";
import * as GQL from "src/core/generated-graphql";
import {
  useGroupCreate,
  useGroupUpdate,
  useSceneUpdate,
} from "src/core/StashService";

type MovieMatchAction =
  | "would_create_group"
  | "would_update_group"
  | "would_upsert_title"
  | "would_link_scene"
  | string;

interface MovieMatchTitleInput {
  language_code: string;
  title: string;
  source?: string;
}

interface MovieMatchSceneGroup {
  group_id: string;
  scene_index?: number | null;
}

interface MovieMatchCandidate {
  Candidate?: {
    TMDBID?: number;
    Title?: string;
    OriginalTitle?: string;
    ReleaseDate?: string;
    PosterURL?: string;
  };
  Score?: number;
}

interface MovieMatchReportItem {
  Action: MovieMatchAction;
  SceneID: string;
  Path: string;
  GroupID?: string;
  GroupName?: string;
  Planned?: string[];
  GroupInput?: Record<string, unknown>;
  TitleInput?: MovieMatchTitleInput;
  SceneGroups?: MovieMatchSceneGroup[];
  Candidate?: MovieMatchCandidate;
  Alternates?: MovieMatchCandidate[];
  Detail?: string;
}

interface MovieMatchReport {
  generated_at?: string;
  mode?: string;
  roots?: string[];
  items: MovieMatchReportItem[];
}

type ApplyState = Record<number, "done" | "error" | "skipped" | undefined>;
type SelectedFieldState = Record<number, Set<string>>;

function isActionable(item: MovieMatchReportItem) {
  return item.Action.startsWith("would_");
}

function itemKey(item: MovieMatchReportItem) {
  return item.Path || item.SceneID;
}

function candidateTitle(candidate?: MovieMatchCandidate["Candidate"]) {
  if (!candidate) return "";
  const year = candidate.ReleaseDate?.slice(0, 4);
  const suffix = year ? ` (${year})` : "";
  return `tmdb:${candidate.TMDBID ?? ""} ${candidate.Title ?? ""}${suffix}`;
}

function candidateOriginalTitle(candidate?: MovieMatchCandidate["Candidate"]) {
  if (
    !candidate?.OriginalTitle ||
    candidate.OriginalTitle === candidate.Title
  ) {
    return "";
  }
  return candidate.OriginalTitle;
}

function scoreLabel(item: MovieMatchReportItem) {
  const score = item.Candidate?.Score;
  return typeof score === "number" ? score.toFixed(2) : "";
}

function groupInputFieldKeys(item: MovieMatchReportItem) {
  return Object.keys(item.GroupInput ?? {}).filter((key) => key !== "id");
}

function isRequiredGroupField(item: MovieMatchReportItem, key: string) {
  return (
    (item.Action === "would_create_group" && key === "name") ||
    key === "custom_fields"
  );
}

function hasGroupMutationFields(input: Record<string, unknown>) {
  return Object.keys(input).some((key) => key !== "id");
}

function plannedFieldLabel(item: MovieMatchReportItem, key: string) {
  return (
    item.Planned?.find((planned) => planned === key || planned.startsWith(`${key}=`)) ??
    key
  );
}

interface NativeCandidateSubset {
  score: number;
  candidate: {
    tmdb_id: number;
    title?: string;
    original_title?: string;
    release_date?: string;
    poster_url?: string;
  };
}

function nativeCandidate(
  candidate?: NativeCandidateSubset | null
): MovieMatchCandidate | undefined {
  if (!candidate) return undefined;
  return {
    Candidate: {
      TMDBID: candidate.candidate.tmdb_id,
      Title: candidate.candidate.title,
      OriginalTitle: candidate.candidate.original_title,
      ReleaseDate: candidate.candidate.release_date,
      PosterURL: candidate.candidate.poster_url,
    },
    Score: candidate.score,
  };
}

function nativeReport(
  result: GQL.MovieMatchPlanQuery["movieMatchPlan"]
): MovieMatchReport {
  return {
    generated_at: result.generated_at,
    mode: result.mode,
    roots: result.roots,
    items: result.items.map((item) => ({
      Action: item.action,
      SceneID: item.scene_id,
      Path: item.path,
      GroupID: item.group_id ?? undefined,
      GroupName: item.group_name ?? undefined,
      Planned: item.planned,
      GroupInput: item.group_input as Record<string, unknown> | undefined,
      TitleInput: item.title_input
        ? {
            language_code: item.title_input.language_code,
            title: item.title_input.title,
            source: item.title_input.source ?? undefined,
          }
        : undefined,
      SceneGroups: item.scene_groups,
      Candidate: nativeCandidate(item.candidate),
      Alternates: item.alternates
        .map((alternate) => nativeCandidate(alternate))
        .filter((alternate): alternate is MovieMatchCandidate => !!alternate),
      Detail: item.detail ?? undefined,
    })),
  };
}

function sceneGroupsWithNewGroup(
  item: MovieMatchReportItem,
  groupID: string
): GQL.SceneGroupInput[] {
  const existing = item.SceneGroups ?? [];
  const seen = new Set<string>();
  const groups: GQL.SceneGroupInput[] = existing
    .filter((group) => {
      if (!group.group_id || seen.has(group.group_id)) return false;
      seen.add(group.group_id);
      return true;
    })
    .map((group) => ({
      group_id: group.group_id,
      scene_index: group.scene_index ?? undefined,
    }));

  if (!seen.has(groupID)) {
    groups.push({ group_id: groupID });
  }

  return groups;
}

function uniqueSceneIDs(items: MovieMatchReportItem[]) {
  return Array.from(
    new Set(
      items
        .map((item) => item.SceneID)
        .filter((sceneID): sceneID is string => !!sceneID)
    )
  );
}

function replaceReportItemsByScene(
  currentItems: MovieMatchReportItem[],
  replacementItems: MovieMatchReportItem[],
  sceneIDs: string[]
) {
  const replaceIDs = new Set(sceneIDs);
  const replacementByScene = new Map<string, MovieMatchReportItem[]>();
  for (const item of replacementItems) {
    if (!replacementByScene.has(item.SceneID)) {
      replacementByScene.set(item.SceneID, []);
    }
    replacementByScene.get(item.SceneID)?.push(item);
  }

  const next: MovieMatchReportItem[] = [];
  const inserted = new Set<string>();
  for (const item of currentItems) {
    if (!replaceIDs.has(item.SceneID)) {
      next.push(item);
      continue;
    }
    if (inserted.has(item.SceneID)) {
      continue;
    }
    next.push(...(replacementByScene.get(item.SceneID) ?? []));
    inserted.add(item.SceneID);
  }

  for (const sceneID of sceneIDs) {
    if (!inserted.has(sceneID)) {
      next.push(...(replacementByScene.get(sceneID) ?? []));
    }
  }

  return next;
}

export const MovieMatchReview: React.FC = () => {
  const [report, setReport] = useState<MovieMatchReport>();
  const [selected, setSelected] = useState<Set<number>>(new Set());
  const [skipped, setSkipped] = useState<Set<number>>(new Set());
  const [selectedFields, setSelectedFields] = useState<SelectedFieldState>({});
  const [applyState, setApplyState] = useState<ApplyState>({});
  const [error, setError] = useState<string>();
  const [roots, setRoots] = useState("");
  const [tmdbToken, setTMDBToken] = useState("");
  const [minConfidence, setMinConfidence] = useState(0.9);
  const [overwrite, setOverwrite] = useState(false);
  const [createdGroupIDs, setCreatedGroupIDs] = useState<
    Record<string, string>
  >({});

  const [loadMovieMatchPlan, movieMatchPlan] = GQL.useMovieMatchPlanLazyQuery({
    fetchPolicy: "network-only",
  });
  const [createGroup] = useGroupCreate();
  const [updateGroup] = useGroupUpdate();
  const [sceneUpdate] = useSceneUpdate();
  const [upsertLocalizedTitle] = GQL.useLocalizedTitleUpsertMutation();

  const items = report?.items ?? [];
  const actionableCount = useMemo(
    () => items.filter((item) => isActionable(item)).length,
    [items]
  );
  const unskippedActionableCount = useMemo(
    () =>
      items.filter((item, index) => isActionable(item) && !skipped.has(index))
        .length,
    [items, skipped]
  );
  const selectedFieldCount = useMemo(
    () =>
      Object.values(selectedFields).reduce(
        (count, fields) => count + fields.size,
        0
      ),
    [selectedFields]
  );

  function loadReportText(text: string) {
    const parsed = JSON.parse(text) as MovieMatchReport;
    loadReport(parsed);
  }

  function loadReport(parsed: MovieMatchReport) {
    if (!Array.isArray(parsed.items)) {
      throw new Error("Report is missing an items array");
    }
    const nextFields: SelectedFieldState = {};
    parsed.items.forEach((item, index) => {
      const keys = groupInputFieldKeys(item);
      if (keys.length > 0) {
        nextFields[index] = new Set(keys);
      }
    });
    setReport(parsed);
    setApplyState({});
    setSkipped(new Set());
    setSelectedFields(nextFields);
    setCreatedGroupIDs({});
    setSelected(
      new Set(
        parsed.items
          .map((item, index) => (isActionable(item) ? index : -1))
          .filter((index) => index >= 0)
      )
    );
  }

  async function onFileChange(event: React.ChangeEvent<HTMLInputElement>) {
    const file = event.currentTarget.files?.[0];
    if (!file) return;

    try {
      setError(undefined);
      loadReportText(await file.text());
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      event.currentTarget.value = "";
    }
  }

  async function generatePlan() {
    const parsedRoots = roots
      .split(/\r?\n/)
      .map((root) => root.trim())
      .filter(Boolean);
    if (parsedRoots.length === 0) {
      setError("Enter at least one root");
      return;
    }
    const token = tmdbToken.trim();

    try {
      setError(undefined);
      const result = await loadMovieMatchPlan({
        variables: {
          input: {
            roots: parsedRoots,
            ...(token ? { tmdb_token: token } : {}),
            min_confidence: minConfidence,
            overwrite,
          },
        },
      });
      const plan = result.data?.movieMatchPlan;
      if (!plan) throw new Error("Movie match plan returned no data");
      loadReport(nativeReport(plan));
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    }
  }

  async function generateScenePlan(sceneIDs: string[]) {
    if (sceneIDs.length === 0) {
      throw new Error("Select at least one scene to rematch");
    }
    const token = tmdbToken.trim();
    const result = await loadMovieMatchPlan({
      variables: {
        input: {
          scene_ids: sceneIDs,
          ...(token ? { tmdb_token: token } : {}),
          min_confidence: minConfidence,
          overwrite,
        },
      },
    });
    const plan = result.data?.movieMatchPlan;
    if (!plan) throw new Error("Movie match plan returned no data");
    return nativeReport(plan);
  }

  async function rematchSceneIDs(sceneIDs: string[]) {
    try {
      setError(undefined);
      const refreshed = await generateScenePlan(sceneIDs);
      if (!report) {
        loadReport(refreshed);
        return;
      }
      loadReport({
        ...report,
        generated_at: refreshed.generated_at,
        mode: refreshed.mode,
        items: replaceReportItemsByScene(
          report.items,
          refreshed.items,
          sceneIDs
        ),
      });
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    }
  }

  function selectedSceneIDs() {
    return uniqueSceneIDs(
      items.filter((_, index) => selected.has(index) && !skipped.has(index))
    );
  }

  function toggle(index: number) {
    const next = new Set(selected);
    if (next.has(index)) {
      next.delete(index);
    } else {
      next.add(index);
    }
    setSelected(next);
  }

  function selectActionableRows() {
    setSelected(
      new Set(
        items
          .map((item, index) =>
            isActionable(item) && !skipped.has(index) ? index : -1
          )
          .filter((index) => index >= 0)
      )
    );
  }

  function clearSelectedRows() {
    setSelected(new Set());
  }

  function toggleSkipped(index: number) {
    const nextSkipped = new Set(skipped);
    const nextSelected = new Set(selected);
    if (nextSkipped.has(index)) {
      nextSkipped.delete(index);
    } else {
      nextSkipped.add(index);
      nextSelected.delete(index);
    }
    setSkipped(nextSkipped);
    setSelected(nextSelected);
  }

  function toggleField(index: number, key: string) {
    const item = items[index];
    if (!item || isRequiredGroupField(item, key)) return;

    const current = selectedFields[index] ?? new Set(groupInputFieldKeys(item));
    const nextSet = new Set(current);
    if (nextSet.has(key)) {
      nextSet.delete(key);
    } else {
      nextSet.add(key);
    }
    setSelectedFields({
      ...selectedFields,
      [index]: nextSet,
    });
  }

  function setAllOptionalGroupFields(enabled: boolean) {
    const nextFields: SelectedFieldState = {};
    items.forEach((item, index) => {
      const keys = groupInputFieldKeys(item);
      if (keys.length === 0) return;

      nextFields[index] = new Set(
        keys.filter(
          (key) => enabled || isRequiredGroupField(item, key)
        )
      );
    });
    setSelectedFields(nextFields);
  }

  function resolveGroupID(
    item: MovieMatchReportItem,
    applyCreatedGroupIDs?: Record<string, string>
  ) {
    return (
      item.GroupID ||
      applyCreatedGroupIDs?.[itemKey(item)] ||
      createdGroupIDs[itemKey(item)]
    );
  }

  function groupInputForApply(item: MovieMatchReportItem, index: number) {
    if (!item.GroupInput) return undefined;

    const fields =
      selectedFields[index] ?? new Set(groupInputFieldKeys(item));
    const input: Record<string, unknown> = {};
    for (const [key, value] of Object.entries(item.GroupInput)) {
      if (
        key === "id" ||
        fields.has(key) ||
        isRequiredGroupField(item, key)
      ) {
        input[key] = value;
      }
    }
    return input;
  }

  function renderCandidate(item: MovieMatchReportItem) {
    const candidate = item.Candidate?.Candidate;
    if (!candidate) return null;
    const originalTitle = candidateOriginalTitle(candidate);

    return (
      <div className="movie-match-review__candidate">
        {candidate.PosterURL && (
          <img alt="" loading="lazy" src={candidate.PosterURL} />
        )}
        <div>
          <div>{candidateTitle(candidate)}</div>
          {originalTitle && (
            <div className="movie-match-review__muted">{originalTitle}</div>
          )}
          {(item.Alternates?.length ?? 0) > 0 && (
            <div className="movie-match-review__alternates">
              {item.Alternates?.map((alternate) => (
                <div key={alternate.Candidate?.TMDBID}>
                  {candidateTitle(alternate.Candidate)}
                  {typeof alternate.Score === "number" &&
                    ` (${alternate.Score.toFixed(2)})`}
                </div>
              ))}
            </div>
          )}
        </div>
      </div>
    );
  }

  async function applyItem(
    item: MovieMatchReportItem,
    index: number,
    applyCreatedGroupIDs: Record<string, string>
  ) {
    switch (item.Action) {
      case "would_create_group": {
        if (!item.GroupInput) throw new Error("Missing group input");
        const input = groupInputForApply(item, index);
        if (!input) throw new Error("Missing group input");
        const result = await createGroup({
          variables: { input: input as GQL.GroupCreateInput },
        });
        const id = result.data?.groupCreate?.id;
        if (!id) throw new Error("Group create returned no id");
        applyCreatedGroupIDs[itemKey(item)] = id;
        setCreatedGroupIDs((current) => ({
          ...current,
          [itemKey(item)]: id,
        }));
        return id;
      }
      case "would_update_group": {
        const id = item.GroupID;
        if (!id) throw new Error("Missing group id");
        if (!item.GroupInput) throw new Error("Missing group input");
        const input = groupInputForApply(item, index);
        if (!input || !hasGroupMutationFields(input)) return id;
        await updateGroup({
          variables: {
            input: { ...input, id } as GQL.GroupUpdateInput,
          },
        });
        return id;
      }
      case "would_upsert_title": {
        const groupID = resolveGroupID(item, applyCreatedGroupIDs);
        if (!groupID) throw new Error("Missing group id for title");
        if (!item.TitleInput) throw new Error("Missing localized title input");
        await upsertLocalizedTitle({
          variables: {
            input: {
              object_type: GQL.LocalizedTitleObjectType.Group,
              object_id: groupID,
              language_code: item.TitleInput.language_code,
              title: item.TitleInput.title,
              source: item.TitleInput.source,
            },
          },
        });
        return groupID;
      }
      case "would_link_scene": {
        const groupID = resolveGroupID(item, applyCreatedGroupIDs);
        if (!groupID) throw new Error("Missing group id for scene link");
        await sceneUpdate({
          variables: {
            input: {
              id: item.SceneID,
              groups: sceneGroupsWithNewGroup(item, groupID),
            },
          },
        });
        return groupID;
      }
      default:
        throw new Error(`Unsupported action ${item.Action}`);
    }
  }

  async function applySelected() {
    setError(undefined);
    const nextState: ApplyState = { ...applyState };
    const applyCreatedGroupIDs: Record<string, string> = { ...createdGroupIDs };

    for (const [index, item] of items.entries()) {
      if (skipped.has(index)) {
        nextState[index] = "skipped";
        setApplyState({ ...nextState });
        continue;
      }
      if (!selected.has(index)) continue;
      try {
        await applyItem(item, index, applyCreatedGroupIDs);
        nextState[index] = "done";
      } catch (e) {
        nextState[index] = "error";
        setApplyState({ ...nextState });
        setError(e instanceof Error ? e.message : String(e));
        return;
      }
      setApplyState({ ...nextState });
    }
  }

  return (
    <div className="movie-match-review">
      <div className="movie-match-review__header">
        <h2>Movie Match</h2>
        <div className="movie-match-review__actions">
          <Form.Control
            as="textarea"
            rows={2}
            placeholder="Roots"
            value={roots}
            onChange={(event) => setRoots(event.currentTarget.value)}
          />
          <Form.Control
            type="password"
            placeholder="TMDB token override"
            value={tmdbToken}
            onChange={(event) => setTMDBToken(event.currentTarget.value)}
          />
          <Form.Control
            className="movie-match-review__confidence"
            max={1}
            min={0}
            step={0.01}
            type="number"
            value={minConfidence}
            onChange={(event) =>
              setMinConfidence(Number(event.currentTarget.value))
            }
          />
          <Form.Check
            id="movie-match-overwrite"
            checked={overwrite}
            label="Overwrite"
            onChange={(event) => setOverwrite(event.currentTarget.checked)}
          />
          <Button
            variant="secondary"
            disabled={movieMatchPlan.loading}
            onClick={() => void generatePlan()}
          >
            Generate plan
          </Button>
          <Form.File
            id="movie-match-report"
            label="Import report"
            custom
            accept="application/json,.json"
            onChange={onFileChange}
          />
          <Button
            variant="primary"
            disabled={selected.size === 0}
            onClick={() => void applySelected()}
          >
            Apply selected
          </Button>
        </div>
      </div>

      {error && <Alert variant="danger">{error}</Alert>}
      {movieMatchPlan.error && (
        <Alert variant="danger">{movieMatchPlan.error.message}</Alert>
      )}

      {report && (
        <div className="movie-match-review__review-bar">
          <div className="movie-match-review__summary">
            <span>{items.length} rows</span>
            <span>{actionableCount} actionable</span>
            <span>{unskippedActionableCount} unskipped</span>
            <span>{selected.size} selected</span>
            <span>{selectedFieldCount} fields</span>
            {report.generated_at && <span>{report.generated_at}</span>}
          </div>
          <div className="movie-match-review__bulk-actions">
            <Button
              size="sm"
              variant="secondary"
              disabled={movieMatchPlan.loading || items.length === 0}
              onClick={() => void rematchSceneIDs(uniqueSceneIDs(items))}
            >
              Rematch all
            </Button>
            <Button
              size="sm"
              variant="secondary"
              disabled={movieMatchPlan.loading || selected.size === 0}
              onClick={() => void rematchSceneIDs(selectedSceneIDs())}
            >
              Rematch selected
            </Button>
            <Button size="sm" variant="secondary" onClick={selectActionableRows}>
              Select actionable
            </Button>
            <Button size="sm" variant="secondary" onClick={clearSelectedRows}>
              Clear rows
            </Button>
            <Button
              size="sm"
              variant="secondary"
              onClick={() => setSkipped(new Set())}
            >
              Unskip all
            </Button>
            <Button
              size="sm"
              variant="secondary"
              onClick={() => setAllOptionalGroupFields(true)}
            >
              Select fields
            </Button>
            <Button
              size="sm"
              variant="secondary"
              onClick={() => setAllOptionalGroupFields(false)}
            >
              Clear fields
            </Button>
          </div>
        </div>
      )}

      <Table
        className="movie-match-review__table"
        bordered
        hover
        responsive
        size="sm"
      >
        <thead>
          <tr>
            <th />
            <th>Action</th>
            <th>Path</th>
            <th>Match</th>
            <th>Score</th>
            <th>Group</th>
            <th>Planned</th>
            <th>Review</th>
            <th>Status</th>
          </tr>
        </thead>
        <tbody>
          {items.map((item, index) => (
            <tr key={`${item.SceneID}-${item.Action}-${index}`}>
              <td>
                <Form.Check
                  checked={selected.has(index)}
                  disabled={!isActionable(item) || skipped.has(index)}
                  onChange={() => toggle(index)}
                  aria-label={`Select ${item.Action}`}
                />
              </td>
              <td>{item.Action}</td>
              <td className="movie-match-review__path">{item.Path}</td>
              <td>{renderCandidate(item)}</td>
              <td>{scoreLabel(item)}</td>
              <td>{item.GroupID || item.GroupName || "-"}</td>
              <td>
                {groupInputFieldKeys(item).length > 0 ? (
                  <div className="movie-match-review__fields">
                    {groupInputFieldKeys(item).map((key) => (
                      <Form.Check
                        key={key}
                        checked={
                          isRequiredGroupField(item, key) ||
                          (
                            selectedFields[index] ??
                            new Set(groupInputFieldKeys(item))
                          ).has(key)
                        }
                        disabled={isRequiredGroupField(item, key)}
                        id={`movie-match-field-${index}-${key}`}
                        label={plannedFieldLabel(item, key)}
                        onChange={() => toggleField(index, key)}
                      />
                    ))}
                  </div>
                ) : (
                  (item.Planned ?? []).join("; ")
                )}
              </td>
              <td>
                <div className="movie-match-review__row-actions">
                  {isActionable(item) && (
                    <Button
                      size="sm"
                      variant="secondary"
                      onClick={() => toggleSkipped(index)}
                    >
                      {skipped.has(index) ? "Unskip" : "Skip"}
                    </Button>
                  )}
                  <Button
                    size="sm"
                    variant="secondary"
                    disabled={movieMatchPlan.loading}
                    onClick={() => void rematchSceneIDs([item.SceneID])}
                  >
                    Rematch
                  </Button>
                </div>
              </td>
              <td>{applyState[index] ?? item.Detail}</td>
            </tr>
          ))}
        </tbody>
      </Table>
    </div>
  );
};
