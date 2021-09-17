import {
  CircularProgress,
  FormControlLabel,
  FormGroup,
  Switch,
  TextField,
} from "@material-ui/core";
import * as React from "react";
import { useHistory } from "react-router";
import styled from "styled-components";
import Alert from "../components/Alert";
import Button from "../components/Button";
import Page from "../components/Page";
import { AppContext } from "../contexts/AppContext";
import { useRequestState } from "../hooks/common";
import { AddApplicationResponse } from "../lib/api/applications/applications.pb";
import { PageRoute } from "../lib/types";

type Props = {
  className?: string;
};

function AddApplication({ className }: Props) {
  const history = useHistory();
  const { applicationsClient } = React.useContext(AppContext);
  const [formState, setFormState] = React.useState({
    name: "stringly",
    namespace: "wego-system",
    url: "ssh://git@github.com/jpellizzari/stringly.git",
    path: "k8s/overlays/development",
    autoMerge: false,
  });
  const [addRes, loading, error, req] =
    useRequestState<AddApplicationResponse>();

  const handleSubmit = () => {
    req(
      applicationsClient.AddApplication({
        ...formState,
      })
    );
  };

  React.useEffect(() => {
    if (!addRes) {
      return;
    }
    history.push(
      `${PageRoute.ApplicationDetail}?name=${addRes.application.name}`
    );
  }, [addRes]);

  return (
    <Page className={className} title="Add Application">
      {addRes && addRes.success && (
        <Alert severity="success" title="Application added successfully!" />
      )}
      {error && (
        <Alert severity="error" title="Error!" message={error.message} />
      )}
      <form
        onSubmit={(e) => {
          e.preventDefault();
          handleSubmit();
        }}
      >
        <div>
          <TextField
            onChange={(e) => {
              setFormState({
                ...formState,
                name: e.currentTarget.value,
              });
            }}
            required
            id="name"
            label="Name"
            variant="standard"
            value={formState.name}
          />
        </div>
        <div>
          <TextField
            onChange={(e) => {
              setFormState({
                ...formState,
                namespace: e.currentTarget.value,
              });
            }}
            required
            id="namespace"
            label="Kubernetes Namespace"
            variant="standard"
            value={formState.namespace}
          />
        </div>
        <div>
          <TextField
            onChange={(e) => {
              setFormState({
                ...formState,
                url: e.currentTarget.value,
              });
            }}
            required
            id="url"
            label="Repo URL"
            variant="standard"
            value={formState.url}
          />
        </div>
        <div>
          <TextField
            onChange={(e) => {
              setFormState({
                ...formState,
                path: e.currentTarget.value,
              });
            }}
            required
            id="path"
            label="path"
            variant="standard"
            value={formState.path}
          />
        </div>
        <div>
          <FormGroup>
            <FormControlLabel
              control={<Switch value={formState.autoMerge} defaultChecked />}
              label="Auto Merge"
            />
          </FormGroup>
        </div>
        <div>
          {loading ? (
            <CircularProgress />
          ) : (
            <Button variant="outlined" color="primary" type="submit">
              Submit
            </Button>
          )}
        </div>
      </form>
    </Page>
  );
}

export default styled(AddApplication).attrs({
  className: AddApplication.name,
})`
  h2 {
    color: ${(props) => props.theme.colors.black};
  }
`;
